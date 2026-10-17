package main

// Tests for the HTTP MCP transport and the panel API.
// Everything is in-process: no network, no listening socket.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpHTTPHandler builds the full route table (CORS included) for HTTP tests.
func mcpHTTPHandler(t *testing.T) http.Handler {
	t.Helper()
	return corsMiddleware(newTestHandler(t))
}

func doTestGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// mcpAuthHeaders returns the header set matching the CURRENT auth mode, so a
// test never silently depends on the default being on or off.
func mcpAuthHeaders() map[string]string {
	if tok := mcpToken(); tok != "" {
		return map[string]string{"Authorization": "Bearer " + tok}
	}
	return map[string]string{}
}

func doJSON(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 1. Auth is OFF by default (MCP_HTTP_TOKEN unset), which is the documented
// behaviour: the endpoint is open and says so. When a token IS configured,
// every request must present it.
func TestMCPHTTPAuthDefaultOffAndTokenWhenConfigured(t *testing.T) {
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	// ---- default: no token configured → open, and advertised as such ----
	t.Setenv("MCP_HTTP_TOKEN", "")
	h := mcpHTTPHandler(t)
	if mcpAuthEnabled() {
		t.Fatal("auth must be disabled when MCP_HTTP_TOKEN is unset")
	}
	rec := doJSON(t, h, http.MethodPost, "/mcp", call, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("open endpoint: status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/mcp", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET probe = %d, want 200", rec.Code)
	}
	probe := rec.Body.String()
	if !strings.Contains(probe, `"authRequired":false`) {
		t.Errorf("probe must report authRequired=false when open: %s", probe)
	}
	// The open state must be declared, not silent.
	if !strings.Contains(probe, "warning") {
		t.Errorf("open endpoint must carry a risk warning: %s", probe)
	}

	// ---- configured: the token is enforced ----
	t.Setenv("MCP_HTTP_TOKEN", "s3cret-token")
	h = mcpHTTPHandler(t)
	if !mcpAuthEnabled() {
		t.Fatal("auth must be enabled when MCP_HTTP_TOKEN is set")
	}
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"no header", nil},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}},
		{"wrong token", map[string]string{"Authorization": "Bearer nope"}},
		{"token in wrong header", map[string]string{"X-MCP-Token": "nope"}},
	} {
		rec := doJSON(t, h, http.MethodPost, "/mcp", call, tc.headers)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", tc.name, rec.Code)
		}
	}
	if rec := doJSON(t, h, http.MethodGet, "/mcp", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without token = %d, want 401", rec.Code)
	}
	// Correct token, both accepted spellings.
	for _, hdr := range []map[string]string{
		{"Authorization": "Bearer s3cret-token"},
		{"X-MCP-Token": "s3cret-token"},
	} {
		rec := doJSON(t, h, http.MethodPost, "/mcp", call, hdr)
		if rec.Code != http.StatusOK {
			t.Errorf("valid token via %v = %d, want 200", hdr, rec.Code)
		}
	}
}

// 2. A cross-origin browser request is refused even with a valid token.
func TestMCPHTTPRefusesCrossOrigin(t *testing.T) {
	h := mcpHTTPHandler(t)
	crossHeaders := mcpAuthHeaders()
	crossHeaders["Origin"] = "http://evil.example"
	rec := doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, crossHeaders)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	// A loopback origin is allowed.
	loopHeaders := mcpAuthHeaders()
	loopHeaders["Origin"] = "http://127.0.0.1:8812"
	rec = doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, loopHeaders)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback origin = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

// 3. The full JSON-RPC flow works over HTTP, and notifications get 202.
func TestMCPHTTPJSONRPCFlow(t *testing.T) {
	h := mcpHTTPHandler(t)
	auth := mcpAuthHeaders()

	rec := doJSON(t, h, http.MethodPost, "/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize = %d (%s)", rec.Code, rec.Body.String())
	}
	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &init); err != nil {
		t.Fatalf("initialize reply not JSON: %v", err)
	}
	if init.Result.ProtocolVersion != mcpProtocolVersion {
		t.Errorf("protocolVersion = %q", init.Result.ProtocolVersion)
	}
	if init.Result.ServerInfo.Name != mcpServerName {
		t.Errorf("serverInfo.name = %q", init.Result.ServerInfo.Name)
	}

	// tools/list over HTTP
	rec = doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, auth)
	var list struct {
		Result struct {
			Tools []map[string]interface{} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("tools/list reply not JSON: %v", err)
	}
	if len(list.Result.Tools) < 8 {
		t.Errorf("tools over HTTP = %d", len(list.Result.Tools))
	}

	// notification → 202 with no body
	rec = doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, auth)
	if rec.Code != http.StatusAccepted {
		t.Errorf("notification = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("notification returned a body: %q", rec.Body.String())
	}

	// unknown method → JSON-RPC error, still HTTP 200
	rec = doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":3,"method":"ghost"}`, auth)
	if rec.Code != http.StatusOK {
		t.Errorf("unknown method HTTP = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "-32601") {
		t.Errorf("unknown method body = %s", rec.Body.String())
	}

	// malformed JSON → -32700
	rec = doJSON(t, h, http.MethodPost, "/mcp", `{oops`, auth)
	if !strings.Contains(rec.Body.String(), "-32700") {
		t.Errorf("parse error body = %s", rec.Body.String())
	}

	// GET is a capability probe
	rec = doJSON(t, h, http.MethodGet, "/mcp", "", auth)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "streamable-http") {
		t.Errorf("GET probe = %d %s", rec.Code, rec.Body.String())
	}

	// DELETE (session teardown) is a no-op 204
	if rec = doJSON(t, h, http.MethodDelete, "/mcp", "", auth); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", rec.Code)
	}
}

// 4. A tools/call over HTTP reaches the local data path.
func TestMCPHTTPToolCall(t *testing.T) {
	h := mcpHTTPHandler(t)
	rec := doJSON(t, h, http.MethodPost, "/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hunyuan_health","arguments":{}}}`,
		mcpAuthHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call = %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("tools/call reply not JSON: %v", err)
	}
	if len(out.Result.Content) == 0 {
		t.Fatalf("no content: %s", rec.Body.String())
	}
	text := out.Result.Content[0].Text
	if !strings.Contains(text, "hunyuan3d-proxy") || !strings.Contains(text, "upstream") {
		t.Errorf("health text = %s", text)
	}
}

// 5. The panel API is loopback-only and describes the endpoint completely.
func TestMCPPanelAPI(t *testing.T) {
	h := mcpHTTPHandler(t)

	// Simulate a panel call coming from the console's own origin (r.Host is set
	// by httptest; Origin is what the gate checks).
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/panel", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8812")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("panel = %d (%s)", rec.Code, rec.Body.String())
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Transport    string                   `json:"transport"`
			Endpoint     string                   `json:"endpoint"`
			Protocol     string                   `json:"protocol"`
			Token        string                   `json:"token"`
			ToolCount    int                      `json:"toolCount"`
			Tools        []map[string]interface{} `json:"tools"`
			Spends       []string                 `json:"spendsCredit"`
			Stats        map[string]interface{}   `json:"stats"`
			AuthRequired bool                     `json:"authRequired"`
			Warning      string                   `json:"warning"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("panel not JSON: %v", err)
	}
	if !env.OK {
		t.Fatalf("panel ok=false: %s", rec.Body.String())
	}
	d := env.Data
	if d.Transport != "streamable-http" || d.Protocol != mcpProtocolVersion {
		t.Errorf("transport/protocol = %q/%q", d.Transport, d.Protocol)
	}
	if !strings.HasSuffix(d.Endpoint, "/mcp") {
		t.Errorf("endpoint = %q", d.Endpoint)
	}
	// The panel mirrors the real auth mode: with no token configured it must
	// report authRequired=false and explain the exposure instead of inventing
	// a token.
	if d.AuthRequired {
		if d.Token == "" {
			t.Error("auth is required but the panel exposed no token")
		}
	} else {
		if d.Token != "" {
			t.Errorf("auth disabled but panel still reports a token: %q", d.Token)
		}
		if d.Warning == "" {
			t.Error("open endpoint must be reported with a warning")
		}
	}
	if d.ToolCount != len(d.Tools) || d.ToolCount < 8 {
		t.Errorf("toolCount %d vs tools %d", d.ToolCount, len(d.Tools))
	}
	if len(d.Spends) != 2 {
		t.Errorf("spendsCredit = %v, want the two generate tools", d.Spends)
	}
	// Every tool the panel advertises carries a schema the UI can render.
	for _, tool := range d.Tools {
		if name, _ := tool["name"].(string); name == "" {
			t.Errorf("panel tool without a name: %v", tool)
		}
		if _, ok := tool["inputSchema"].(map[string]interface{}); !ok {
			t.Errorf("panel tool %v missing inputSchema", tool["name"])
		}
	}

	// Cross-origin panel access is refused (it would disclose the token).
	req = httptest.NewRequest(http.MethodGet, "/api/mcp/panel", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin panel = %d, want 403", rec.Code)
	}
}

// 6. Panel stats reflect real traffic, so the UI numbers are not fabricated.
func TestMCPPanelStatsTrackCalls(t *testing.T) {
	h := mcpHTTPHandler(t)
	auth := mcpAuthHeaders()

	for i := 0; i < 3; i++ {
		doJSON(t, h, http.MethodPost, "/mcp",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hunyuan_health","arguments":{}}}`, auth)
	}
	rec := doJSON(t, h, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"ghost"}`, auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/panel", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var env struct {
		Data struct {
			Stats map[string]interface{} `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("panel not JSON: %v", err)
	}
	st := env.Data.Stats
	if got := st["toolCalls"].(float64); got != 3 {
		t.Errorf("toolCalls = %v, want 3", got)
	}
	if got := st["requests"].(float64); got != 4 {
		t.Errorf("requests = %v, want 4", got)
	}
	if got := st["errors"].(float64); got != 1 {
		t.Errorf("errors = %v, want 1", got)
	}
	if st["lastTool"] != "hunyuan_health" {
		t.Errorf("lastTool = %v", st["lastTool"])
	}
	if st["lastMethod"] != "ghost" {
		t.Errorf("lastMethod = %v", st["lastMethod"])
	}
}

// 7. The endpoint must not be part of the unauthenticated local surface.
func TestMCPHTTPNotExposedInOpenAPIOrHealth(t *testing.T) {
	h := mcpHTTPHandler(t)
	rec := doTestGet(t, h, "/api/health")
	if strings.Contains(rec.Body.String(), "mcpToken") {
		t.Error("health leaks the MCP token")
	}
	rec = doTestGet(t, h, "/openapi.json")
	if strings.Contains(rec.Body.String(), "\"/mcp\"") {
		t.Error("/mcp should not be advertised as a normal documented API route")
	}
}

// 8. The stdio transport is gone: /mcp is the only way in, and the removed
// flag must explain itself rather than silently starting a wrong server.
func TestMCPStdioRemoved(t *testing.T) {
	if strings.Contains(usageText, "-mcp") {
		t.Error("help text still advertises the removed -mcp mode")
	}
	for _, want := range []string{"POST http://127.0.0.1", "/mcp", "docs/mcp.md"} {
		if !strings.Contains(mcpRemovedNotice, want) {
			t.Errorf("removal notice should mention %q:\n%s", want, mcpRemovedNotice)
		}
	}
	// Nothing in the binary may still reference a stdio entry point.
	if strings.Contains(mcpServerName, "stdio") {
		t.Error("unexpected stdio reference in server identity")
	}
}
