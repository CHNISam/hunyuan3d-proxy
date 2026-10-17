package main

// MCP over HTTP (Streamable HTTP transport) + a small read-only introspection
// API that backs the Web UI panel.
//
// The MCP server therefore runs inside the normal HTTP server and shares the
// live cookie jar, mode switch and credit state with the UI.
//
// Security: this endpoint can spend credits. It is therefore NOT part of the
// open local surface — it requires a token (see mcpToken) and refuses requests
// whose Origin is not the local console.

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// mcpHTTPStats records what the panel shows. Kept intentionally small: the
// panel is a status view, not an audit log of request bodies.
type mcpHTTPStats struct {
	mu          sync.Mutex
	requests    int64
	toolCalls   int64
	errors      int64
	lastMethod  string
	lastTool    string
	lastAt      time.Time
	lastError   string
	initialized bool
}

func (s *mcpHTTPStats) record(method, tool string, failed bool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.lastMethod = method
	s.lastAt = time.Now()
	if method == "tools/call" {
		s.toolCalls++
		s.lastTool = tool
	}
	if method == "initialize" {
		s.initialized = true
	}
	if failed {
		s.errors++
		s.lastError = errMsg
	}
}

func (s *mcpHTTPStats) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]interface{}{
		"requests":    s.requests,
		"toolCalls":   s.toolCalls,
		"errors":      s.errors,
		"lastMethod":  s.lastMethod,
		"lastTool":    s.lastTool,
		"initialized": s.initialized,
	}
	if !s.lastAt.IsZero() {
		out["lastAt"] = s.lastAt.Format(time.RFC3339)
	}
	if s.lastError != "" {
		out["lastError"] = s.lastError
	}
	return out
}

// mcpToken returns the bearer token guarding /mcp.
//
// Auth is OFF by default: MCP_HTTP_TOKEN is opt-in. Set it whenever the proxy
// is reachable beyond loopback — /mcp can spend real credits without a
// confirmation prompt, so an exposed endpoint without a token is an open
// spending surface.
func mcpToken() string {
	return strings.TrimSpace(os.Getenv("MCP_HTTP_TOKEN"))
}

// mcpAuthEnabled reports whether a bearer token is required.
// This is the single source of truth: "off" and "misconfigured" stay distinct.
func mcpAuthEnabled() bool { return mcpToken() != "" }

// mcpAuthorized reports whether the request may proceed.
// With auth disabled (the default) every caller is allowed; with a token
// configured, the bearer value must match exactly.
func mcpAuthorized(r *http.Request) bool {
	want := mcpToken()
	if want == "" {
		return true // auth disabled by configuration
	}
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	got = strings.TrimPrefix(got, "Bearer ")
	got = strings.TrimSpace(got)
	if got == "" {
		got = strings.TrimSpace(r.Header.Get("X-MCP-Token"))
	}
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// mcpLocalOrigin reports whether the request looks like it came from the local
// console. Used as a second gate for browser-originated requests (defence in
// depth against a page on another origin driving the endpoint).
func mcpLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client (curl, MCP client)
	}
	return strings.Contains(origin, "127.0.0.1") ||
		strings.Contains(origin, "localhost") ||
		strings.Contains(origin, "[::1]")
}

// HandleMCP serves the Streamable HTTP MCP endpoint.
//
// JSON-RPC request  → application/json response (single JSON object).
// Notifications     → 202 Accepted with an empty body.
// GET               → endpoint metadata, so a browser/panel can confirm it is live.
func (s *Server) HandleMCP(mcp *mcpServer, stats *mcpHTTPStats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !mcpLocalOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		if !mcpAuthorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			writeErr(w, http.StatusUnauthorized,
				"missing or invalid MCP token; send Authorization: Bearer <token> (token comes from MCP_HTTP_TOKEN; see the MCP panel in the Web UI)")
			return
		}

		switch r.Method {
		case http.MethodGet:
			// Capability probe for humans/UI. Never leaks the token.
			info := map[string]interface{}{
				"transport":    "streamable-http",
				"endpoint":     "/mcp",
				"protocol":     mcpProtocolVersion,
				"server":       mcpServerName,
				"version":      mcpServerVersion,
				"tools":        len(mcp.tools),
				"authRequired": mcpAuthEnabled(),
			}
			if mcpAuthEnabled() {
				info["hint"] = "POST JSON-RPC 2.0 here with Authorization: Bearer <token>"
			} else {
				info["hint"] = "auth is OFF (MCP_HTTP_TOKEN unset): POST JSON-RPC 2.0 directly. " +
					"Tools can spend credits — set MCP_HTTP_TOKEN before exposing this port beyond loopback."
				info["warning"] = "unauthenticated endpoint: anyone who can reach this port can spend the account's credits"
			}
			writeOK(w, info)
			return
		case http.MethodDelete:
			// Session teardown is a no-op (stateless transport).
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodPost:
			// handled below
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			writeErr(w, http.StatusMethodNotAllowed, "GET, POST or DELETE")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
			return
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			writeErr(w, http.StatusBadRequest, "empty body; expected a JSON-RPC message")
			return
		}

		var req rpcRequest
		if uerr := json.Unmarshal(body, &req); uerr != nil {
			stats.record("", "", true, "parse error")
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"jsonrpc": "2.0", "id": nil,
				"error": &rpcError{Code: codeParseError, Message: "parse error: " + uerr.Error()},
			})
			return
		}

		// Notifications get no body: 202 is the spec's answer.
		if req.isNotification() {
			stats.record(req.Method, toolNameOf(req.Params), false, "")
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result, rpcErr := mcp.handle(&req)
		stats.record(req.Method, toolNameOf(req.Params), rpcErr != nil, rpcErrText(rpcErr))

		resp := map[string]interface{}{"jsonrpc": "2.0", "id": json.RawMessage(req.ID)}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else if result == nil {
			resp["result"] = map[string]interface{}{}
		} else {
			resp["result"] = result
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// rpcErrText renders an rpc error for the stats view.
// (Named distinctly from text3d.go's errString, which takes an error.)
func rpcErrText(e *rpcError) string {
	if e == nil {
		return ""
	}
	return e.Message
}

// toolNameOf extracts params.name from a tools/call for the stats view.
func toolNameOf(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	return p.Name
}

// HandleMCPPanel backs the Web UI panel: everything the console shows about
// the MCP endpoint, including the token it must use (the panel is same-origin
// loopback UI, and the token is already visible in this process's environment).
func (s *Server) HandleMCPPanel(mcp *mcpServer, stats *mcpHTTPStats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		if !mcpLocalOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-origin request refused")
			return
		}

		tools := make([]map[string]interface{}, 0, len(mcp.tools))
		for _, t := range mcp.tools {
			tools = append(tools, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}

		base := "http://" + r.Host
		authOn := mcpAuthEnabled()
		resp := map[string]interface{}{
			"enabled":      true,
			"transport":    "streamable-http",
			"endpoint":     base + "/mcp",
			"protocol":     mcpProtocolVersion,
			"server":       mcpServerName,
			"version":      mcpServerVersion,
			"authRequired": authOn,
			"token":        mcpToken(),
			"tools":        tools,
			"toolCount":    len(tools),
			"stats":        stats.snapshot(),
			"spendsCredit": []string{"hunyuan_generate_concept", "hunyuan_generate_main"},
		}
		if authOn {
			resp["authHeader"] = "Authorization: Bearer <token>"
		} else {
			resp["authHeader"] = ""
			resp["warning"] = "令牌未启用（MCP_HTTP_TOKEN 未设置）：任何能访问该端口的程序都能无鉴权调用工具并消耗积分。" +
				"仅在确认端口未被局域网/公网暴露时保持关闭。"
		}
		writeOK(w, resp)
	}
}
