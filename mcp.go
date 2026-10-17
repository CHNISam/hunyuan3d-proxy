package main

// MCP (Model Context Protocol) server core — pure Go, no deps.
//
// This file is transport-agnostic: mcp_http.go exposes it as a Streamable HTTP
// endpoint, and the tests drive it in-process. There is no stdio transport.
//
// Implemented methods:
//
//	initialize               → protocolVersion + tools capability
//	notifications/initialized→ no-op (notification, no reply)
//	tools/list               → the tool catalogue
//	tools/call               → dispatch a tool
//	ping                     → {}
//
// The tool layer (mcp_tools.go) is transport- and data-path-agnostic: it can
// dispatch into an in-process handler or forward to a running proxy.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	mcpProtocolVersion = "2025-06-18"
	mcpServerName      = "hunyuan3d-proxy"
	mcpServerVersion   = "1.0.0"

	// maxToolOutput caps a single tools/call payload so an LLM client never
	// receives a multi-hundred-KB object (the asset board is ~200KB raw).
	maxToolOutput = 128 * 1024
)

// mcpSupportedVersions are echoed back when a client asks for one of them.
var mcpSupportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05", "2024-10-07"}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// JSON-RPC / MCP error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// rpcRequest is an inbound JSON-RPC request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message expects no response.
func (r *rpcRequest) isNotification() bool { return len(r.ID) == 0 }

// mcpTool is one callable tool.
type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(args map[string]interface{}) (interface{}, error)
}

// mcpServer serves the MCP method set over a pair of streams.
type mcpServer struct {
	tools  []mcpTool
	byName map[string]mcpTool

	// call dispatches one local HTTP request against the proxy surface.
	call func(method, path string, body []byte) (*localResult, error)

	// out receives protocol frames; kept injectable for tests.
	logf func(format string, v ...interface{})
}

func newMCPServer(tools []mcpTool, call func(string, string, []byte) (*localResult, error)) *mcpServer {
	byName := make(map[string]mcpTool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	return &mcpServer{
		tools:  tools,
		byName: byName,
		call:   call,
		logf:   func(format string, v ...interface{}) { log.Printf(format, v...) },
	}
}

// ---- method dispatch ----

// handle runs one request and returns (result, nil) or (nil, rpcError).
// A nil result with nil error means "notification: reply with nothing".
func (m *mcpServer) handle(req *rpcRequest) (interface{}, *rpcError) {
	switch req.Method {
	case "initialize":
		return m.initialize(req.Params)
	case "notifications/initialized", "initialized":
		return nil, nil // notification
	case "ping":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{"tools": m.toolCatalogue()}, nil
	case "tools/call":
		return m.toolsCall(req.Params)
	case "resources/list":
		// Advertised as unsupported; answer empty rather than erroring so
		// clients that probe optimistically stay happy.
		return map[string]interface{}{"resources": []interface{}{}}, nil
	case "prompts/list":
		return map[string]interface{}{"prompts": []interface{}{}}, nil
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func (m *mcpServer) initialize(params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)

	version := mcpProtocolVersion
	for _, v := range mcpSupportedVersions {
		if v == p.ProtocolVersion {
			version = v
			break
		}
	}
	return map[string]interface{}{
		"protocolVersion": version,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{"listChanged": false},
		},
		"serverInfo": map[string]interface{}{
			"name":    mcpServerName,
			"version": mcpServerVersion,
		},
		"instructions": "腾讯混元3D 反代服务器（仅学习用途）。生成类工具会真实消耗上游积分；" +
			"先用 hunyuan_login_status / hunyuan_credits 确认会话与额度。" +
			"text3d 生成是异步任务：提交后轮询 hunyuan_assets 查看 status/progress。",
	}, nil
}

// toolCatalogue renders tools in a stable order for tools/list.
func (m *mcpServer) toolCatalogue() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(m.tools))
	for _, t := range m.tools {
		out = append(out, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}

func (m *mcpServer) toolsCall(params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}
	tool, ok := m.byName[p.Name]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]interface{}{}
	}

	payload, err := tool.Handler(p.Arguments)
	if err != nil {
		// Tool-level failure: report as content + isError so the model can
		// read the reason and retry, rather than a transport error.
		return toolErrorResult(err.Error()), nil
	}
	return toolTextResult(payload), nil
}

// toolTextResult renders a tool payload as MCP text content.
func toolTextResult(payload interface{}) map[string]interface{} {
	text := ""
	switch v := payload.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	case nil:
		text = "{}"
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return toolErrorResult("result is not serialisable: " + err.Error())
		}
		text = string(b)
	}
	meta := map[string]interface{}{}
	if len(text) > maxToolOutput {
		cut := text[:maxToolOutput]
		if i := strings.LastIndexAny(cut, "\n"); i > 0 {
			cut = cut[:i]
		}
		text = cut + "\n… output truncated at " + strconv.Itoa(maxToolOutput) + " bytes; pass a smaller limit/offset"
		meta["truncated"] = true
	}
	res := map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
	}
	if len(meta) > 0 {
		res["structuredContent"] = meta
	}
	return res
}

func toolErrorResult(msg string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": "error: " + msg}},
		"isError": true,
	}
}

// write emits one response frame.
func (m *mcpServer) write(w *bufio.Writer, id json.RawMessage, result interface{}, rpcErr *rpcError) {
	msg := map[string]interface{}{"jsonrpc": "2.0"}
	if len(id) > 0 {
		msg["id"] = json.RawMessage(id)
	} else {
		msg["id"] = nil
	}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else if result == nil {
		msg["result"] = map[string]interface{}{}
	} else {
		msg["result"] = result
	}
	enc, err := json.Marshal(msg)
	if err != nil {
		enc, _ = json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0", "id": nil,
			"error": rpcError{Code: codeInternalError, Message: "marshal failure"},
		})
	}
	w.Write(enc)
	w.WriteByte('\n')
	w.Flush()
}

// ---- local data path ----

// localResult is a completed local HTTP response.
type localResult struct {
	Status int
	Body   []byte
	Header http.Header
}

// localRecorder is a minimal http.ResponseWriter that buffers output.
// Handlers in this project never stream, so no Flusher is required.
type localRecorder struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	max       int
	truncated bool
}

func newLocalRecorder(max int) *localRecorder {
	return &localRecorder{header: make(http.Header), status: http.StatusOK, max: max}
}

func (r *localRecorder) Header() http.Header { return r.header }

func (r *localRecorder) WriteHeader(code int) {
	if r.status == http.StatusOK {
		r.status = code
	}
}

func (r *localRecorder) Write(p []byte) (int, error) {
	if r.max > 0 && r.body.Len()+len(p) > r.max {
		room := r.max - r.body.Len()
		if room > 0 {
			r.body.Write(p[:room])
			r.truncated = true
		}
		return len(p), nil // pretend we took it all; caller only needs the prefix
	}
	return r.body.Write(p)
}

// inProcessCall dispatches against a handler without touching the network.
func inProcessCall(h http.Handler, method, path string, body []byte) (*localResult, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://local"+path, rdr)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := newLocalRecorder(4 << 20)
	h.ServeHTTP(rec, req)
	return &localResult{Status: rec.status, Body: rec.body.Bytes(), Header: rec.header}, nil
}

// httpCall forwards to a running proxy (MCP_PROXY_BASE).
func httpCall(client *http.Client, base, method, path string, body []byte) (*localResult, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, rdr)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy unreachable at %s: %w", base, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return &localResult{Status: resp.StatusCode, Body: b, Header: resp.Header}, nil
}

// decodeEnvelopeJSON unwraps the proxy's {ok,data,error} envelope.
func decodeEnvelopeJSON(raw []byte) (map[string]interface{}, error) {
	var env map[string]interface{}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	return env, nil
}

// sortedKeys is a small helper used by tools that render stable summaries.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
