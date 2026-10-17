package main

// Offline tests for the MCP layer. No network: tool calls are dispatched
// against a stub handler so assertions stay deterministic.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubProxy answers like the real proxy does: {ok,data,error} envelopes.
func stubProxy(t *testing.T, respond func(r *http.Request) (int, map[string]interface{})) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, data := respond(r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": status < 400, "data": data})
	})
}

func stubServer(t *testing.T, respond func(r *http.Request) (int, map[string]interface{})) *mcpServer {
	t.Helper()
	h := stubProxy(t, respond)
	call := func(method, path string, body []byte) (*localResult, error) {
		return inProcessCall(h, method, path, body)
	}
	return newMCPServer(mcpToolSet(call), call)
}

// runFrames posts one JSON-RPC frame per call through the real HTTP handler and
// returns the replies. Notifications produce no reply (HTTP 202), matching the
// transport's contract.
func runFrames(t *testing.T, s *mcpServer, frames ...string) []map[string]interface{} {
	t.Helper()
	stats := &mcpHTTPStats{}
	handler := (&Server{Cfg: Config{Port: "0", Upstream: DefaultUpstream}}).HandleMCP(s, stats)

	var msgs []map[string]interface{}
	for _, frame := range frames {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(frame))
		req.Header.Set("Content-Type", "application/json")
		handler(rec, req)

		switch rec.Code {
		case http.StatusAccepted:
			continue // notification: no reply by design
		case http.StatusOK:
			// fall through to decode
		default:
			t.Fatalf("frame %q → HTTP %d (%s)", frame, rec.Code, rec.Body.String())
		}

		var m map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("reply is not JSON: %q (%v)", rec.Body.String(), err)
		}
		if m["jsonrpc"] != "2.0" {
			t.Fatalf("missing jsonrpc 2.0: %v", m)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func callTool(t *testing.T, s *mcpServer, id, name string, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":`+id+`,"method":"tools/call","params":`+string(payload)+`}`)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(msgs))
	}
	result, ok := msgs[0]["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("no result object: %v", msgs[0])
	}
	return result
}

func toolText(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("no content array: %v", result)
	}
	first, _ := content[0].(map[string]interface{})
	text, _ := first["text"].(string)
	return text
}

// 1. Version negotiation: echo supported versions, fall back to latest.
func TestMCPInitializeNegotiation(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })

	for _, tc := range []struct{ asked, want string }{
		{"2024-11-05", "2024-11-05"},
		{"2025-06-18", "2025-06-18"},
		{"1999-01-01", mcpProtocolVersion}, // unknown → our latest
		{"", mcpProtocolVersion},           // absent  → our latest
	} {
		frame := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + tc.asked + `"}}`
		msgs := runFrames(t, s, frame)
		if len(msgs) != 1 {
			t.Fatalf("asked %q: got %d replies", tc.asked, len(msgs))
		}
		r := msgs[0]["result"].(map[string]interface{})
		if r["protocolVersion"] != tc.want {
			t.Errorf("asked %q → %v, want %s", tc.asked, r["protocolVersion"], tc.want)
		}
		info := r["serverInfo"].(map[string]interface{})
		if info["name"] != mcpServerName {
			t.Errorf("serverInfo.name = %v", info["name"])
		}
		caps := r["capabilities"].(map[string]interface{})
		if _, ok := caps["tools"]; !ok {
			t.Errorf("tools capability missing: %v", caps)
		}
	}
}

// 2. Notifications must not produce a reply.
func TestMCPNotificationProducesNoReply(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })
	msgs := runFrames(t, s,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
	)
	if len(msgs) != 1 {
		t.Fatalf("notifications produced replies: got %d frames, want 1", len(msgs))
	}
	if msgs[0]["id"].(float64) != 9 {
		t.Fatalf("unexpected id: %v", msgs[0]["id"])
	}
}

// 3. Every advertised tool is a well-formed MCP tool.
func TestMCPToolCatalogueIsWellFormed(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })
	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools, ok := msgs[0]["result"].(map[string]interface{})["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list returned nothing: %v", msgs[0])
	}
	if len(tools) < 8 {
		t.Errorf("expected the full tool set, got %d", len(tools))
	}
	seen := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]interface{})
		name, _ := tool["name"].(string)
		if name == "" || strings.ContainsAny(name, " \t") {
			t.Errorf("bad tool name %q", name)
		}
		if seen[name] {
			t.Errorf("duplicate tool name %q", name)
		}
		seen[name] = true
		if desc, _ := tool["description"].(string); len(desc) < 10 {
			t.Errorf("%s: description too short (%q)", name, desc)
		}
		schema, _ := tool["inputSchema"].(map[string]interface{})
		if schema == nil {
			t.Errorf("%s: missing inputSchema", name)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s: inputSchema.type = %v, want object", name, schema["type"])
		}
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			t.Errorf("%s: inputSchema has no properties", name)
		}
		// required must be a subset of declared properties
		if req, ok := schema["required"].([]string); ok {
			for _, r := range req {
				if _, found := props[r]; !found {
					t.Errorf("%s: required %q not declared in properties", name, r)
				}
			}
		}
		if _, err := json.Marshal(schema); err != nil {
			t.Errorf("%s: schema not serialisable: %v", name, err)
		}
		// enum properties must actually carry their values
		for pname, praw := range props {
			p, _ := praw.(map[string]interface{})
			if p == nil {
				continue
			}
			if raw, hasEnum := p["enum"]; hasEnum {
				// schemas are compared post-serialisation, so enum is []interface{}
				var vals []interface{}
				switch v := raw.(type) {
				case []string:
					for _, x := range v {
						vals = append(vals, x)
					}
				case []interface{}:
					vals = v
				}
				if len(vals) == 0 {
					t.Errorf("%s.%s: empty enum", name, pname)
				}
				for _, v := range vals {
					if str, ok := v.(string); !ok || str == "" {
						t.Errorf("%s.%s: bad enum value %v", name, pname, v)
					}
				}
			}
		}
	}
	for _, want := range []string{"hunyuan_health", "hunyuan_assets", "hunyuan_generate_concept", "hunyuan_texture"} {
		if !seen[want] {
			t.Errorf("tool %s missing from catalogue", want)
		}
	}
}

// 4. Unknown method / tool must use the documented JSON-RPC codes.
func TestMCPProtocolErrors(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })

	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":1,"method":"does/not/exist"}`)
	errObj, ok := msgs[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %v", msgs[0])
	}
	if int(errObj["code"].(float64)) != codeMethodNotFound {
		t.Errorf("unknown method code = %v, want %d", errObj["code"], codeMethodNotFound)
	}

	msgs = runFrames(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ghost_tool"}}`)
	errObj = msgs[0]["error"].(map[string]interface{})
	if int(errObj["code"].(float64)) != codeInvalidParams {
		t.Errorf("unknown tool code = %v, want %d", errObj["code"], codeInvalidParams)
	}

	// malformed JSON must not kill the loop, and reports -32700
	msgs = runFrames(t, s, `{not json}`, `{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if len(msgs) != 2 {
		t.Fatalf("parse error should not abort the stream: got %d frames", len(msgs))
	}
	errObj = msgs[0]["error"].(map[string]interface{})
	if int(errObj["code"].(float64)) != codeParseError {
		t.Errorf("parse error code = %v, want %d", errObj["code"], codeParseError)
	}
	if msgs[0]["id"] != nil {
		t.Errorf("parse-error reply must carry id null, got %v", msgs[0]["id"])
	}
	if _, ok := msgs[1]["result"]; !ok {
		t.Errorf("loop did not recover after a bad frame: %v", msgs[1])
	}
}

// 5. A tool call reaches the data path and renders the envelope as text.
func TestMCPToolCallDispatches(t *testing.T) {
	var gotPath, gotMethod string
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		gotPath, gotMethod = r.URL.Path, r.Method
		return 200, map[string]interface{}{
			"service": "hunyuan3d-proxy", "status": "up", "upstream": DefaultUpstream,
			"cookieCount": 3, "sessionLike": true, "signKey": "abc", "noise": "dropped",
		}
	})

	result := callTool(t, s, "1", "hunyuan_health", nil)
	text := toolText(t, result)
	if gotMethod != http.MethodGet || gotPath != "/api/health" {
		t.Fatalf("dispatched %s %s, want GET /api/health", gotMethod, gotPath)
	}
	if result["isError"] == true {
		t.Fatalf("unexpected tool error: %s", text)
	}
	for _, want := range []string{"hunyuan3d-proxy", "upstream"} {
		if !strings.Contains(text, want) {
			t.Errorf("health output missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "noise") {
		t.Errorf("health output should be projected, got %s", text)
	}
}

// 6. A failing local endpoint becomes readable tool content, not a transport error.
func TestMCPToolFailureIsContent(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 500, nil })
	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hunyuan_assets","arguments":{}}}`)
	if _, hasErr := msgs[0]["error"]; hasErr {
		t.Fatalf("handler failure must not be a JSON-RPC error: %v", msgs[0])
	}
	result := msgs[0]["result"].(map[string]interface{})
	if result["isError"] != true {
		t.Fatalf("isError not set: %v", result)
	}
	if !strings.Contains(toolText(t, result), "error:") {
		t.Fatalf("error text missing: %s", toolText(t, result))
	}
}

// 7. The asset board is projected down to the fields a model needs.
func TestMCPAssetsCompaction(t *testing.T) {
	bigCredits := map[string]interface{}{"main": map[string]interface{}{"blob": strings.Repeat("x", 5000)}}
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		if !strings.HasPrefix(r.URL.Path, "/api/text3d/assets") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "3" {
			t.Errorf("limit not forwarded: %q", got)
		}
		return 200, map[string]interface{}{
			"totalCount": 68,
			"items": []interface{}{
				map[string]interface{}{
					"source": "concept", "id": "task-1", "title": "robot", "status": "success",
					"progress": 100, "primaryModelUrl": "https://cdn/model.glb",
					"previewUrl": "https://cdn/preview.png",
					"assets":     map[string]interface{}{"a": "1", "b": "2"},
					"q_version":  "noise",
				},
			},
			"credits":   bigCredits,
			"endpoints": map[string]interface{}{"x": "y"},
		}
	})

	result := callTool(t, s, "1", "hunyuan_assets", map[string]interface{}{"limit": 3, "source": "concept"})
	text := toolText(t, result)
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("asset output is not JSON: %v", err)
	}
	if out["totalCount"].(float64) != 68 {
		t.Errorf("totalCount lost: %v", out["totalCount"])
	}
	items := out["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["primaryModelUrl"] != "https://cdn/model.glb" {
		t.Errorf("primaryModelUrl missing: %v", item)
	}
	if item["assetCount"].(float64) != 2 {
		t.Errorf("assetCount = %v, want 2", item["assetCount"])
	}
	for _, unwanted := range []string{"credits", "endpoints", "noise", "q_version"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("compaction leaked %q (%d bytes)", unwanted, len(text))
		}
	}
	if len(text) > 1200 {
		t.Errorf("compacted output still %d bytes", len(text))
	}
}

// 8. Oversized tool output is truncated before it reaches the model.
// hunyuan_works passes upstream rows through, so an oversized upstream payload
// is the realistic way to hit the cap.
func TestMCPOutputTruncation(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) {
		rows := make([]interface{}, 0, 40)
		for i := 0; i < 40; i++ {
			rows = append(rows, map[string]interface{}{
				"worksId": "w", "prompt": strings.Repeat("y", maxToolOutput/20),
			})
		}
		return 200, map[string]interface{}{"count": len(rows), "works": rows}
	})
	result := callTool(t, s, "1", "hunyuan_works", map[string]interface{}{"limit": 40})
	text := toolText(t, result)
	if len(text) > maxToolOutput+200 {
		t.Fatalf("output not capped: %d bytes", len(text))
	}
	if !strings.Contains(text, "truncated") {
		t.Errorf("truncation not reported: %s", text[max(0, len(text)-160):])
	}
	if result["structuredContent"] == nil {
		t.Errorf("truncation should be flagged in structuredContent")
	}
}

// 9. Argument validation happens before any upstream call.
func TestMCPArgumentValidation(t *testing.T) {
	calls := 0
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) {
		calls++
		return 200, map[string]interface{}{}
	})

	if res := callTool(t, s, "1", "hunyuan_generate_concept", map[string]interface{}{}); res["isError"] != true {
		t.Errorf("empty prompt must fail: %v", res)
	}
	if res := callTool(t, s, "2", "hunyuan_generate_main", map[string]interface{}{"prompt": "   "}); res["isError"] != true {
		t.Errorf("blank prompt must fail: %v", res)
	}
	if res := callTool(t, s, "3", "hunyuan_set_mode", map[string]interface{}{"mode": "nonsense"}); res["isError"] != true {
		t.Errorf("bad mode must fail: %v", res)
	}
	if res := callTool(t, s, "4", "hunyuan_texture", map[string]interface{}{}); res["isError"] != true {
		t.Errorf("texture without worksId/fbxUrl must fail: %v", res)
	}
	if calls != 0 {
		t.Errorf("%d upstream calls escaped validation", calls)
	}
}

// 10. Invalid enum input falls back to the documented default.
func TestMCPEnumFallback(t *testing.T) {
	var sawPath string
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		sawPath = r.URL.Path
		if q := r.URL.RawQuery; q != "" {
			sawPath += "?" + q
		}
		return 200, map[string]interface{}{}
	})
	callTool(t, s, "1", "hunyuan_assets", map[string]interface{}{"source": "everything"})
	if !strings.Contains(sawPath, "source=all") {
		t.Errorf("source fallback → %s, want source=all", sawPath)
	}
	callTool(t, s, "2", "hunyuan_assets", map[string]interface{}{"source": "concept"})
	if !strings.Contains(sawPath, "source=concept") {
		t.Errorf("source concept → %s", sawPath)
	}
}

// 11. Regression: /api/text3d/works returns its array under "works", not "list".
// Reading the wrong key made the tool report an empty board for every query.
func TestMCPWorksReadsWorksKey(t *testing.T) {
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		return 200, map[string]interface{}{
			"upstreamPath": "/api/game3d/general_info/get_works_list",
			"count":        2,
			"filter":       "",
			"works": []interface{}{
				map[string]interface{}{
					"worksId": "wid-1", "worksPipeline": 2, "pipelineStatus": 3, "status": "success",
					"prompt": "lamp", "model": "m.glb", "resultUrls": map[string]interface{}{"geometry": "https://cdn/x.glb"},
				},
				map[string]interface{}{
					"worksId": "wid-2", "worksPipeline": 1, "pipelineStatus": 1, "status": "processing",
					"prompt": "robot",
				},
			},
		}
	})

	result := callTool(t, s, "1", "hunyuan_works", map[string]interface{}{"limit": 10})
	var out map[string]interface{}
	text := toolText(t, result)
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("works output is not JSON: %v", err)
	}
	items, _ := out["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("works items = %d, want 2 (the \"works\" key must be read): %s", len(items), text)
	}
	first := items[0].(map[string]interface{})
	if first["worksId"] != "wid-1" {
		t.Errorf("worksId lost: %v", first)
	}
	if _, ok := first["resultUrls"]; !ok {
		t.Errorf("resultUrls must survive the projection: %v", first)
	}
	if out["totalOnPage"].(float64) != 2 {
		t.Errorf("totalOnPage = %v, want 2", out["totalOnPage"])
	}

	// worksId filter still narrows the list
	result = callTool(t, s, "2", "hunyuan_works", map[string]interface{}{"worksId": "wid-2"})
	out = map[string]interface{}{}
	_ = json.Unmarshal([]byte(toolText(t, result)), &out)
	if got := out["items"].([]interface{}); len(got) != 1 {
		t.Fatalf("filtered items = %d, want 1", len(got))
	}
	if out["filtered"] != true {
		t.Errorf("filtered flag not set")
	}
}

// 12. Submit responses carry the task identifiers, not the credit blobs.
func TestMCPSubmitSummaryDropsCreditNoise(t *testing.T) {
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		return 200, map[string]interface{}{
			"requestId": "req-1", "submittedAt": "2026-10-17T00:00:00+08:00",
			"mode": "text-geometry", "modelVersion": "v3.1", "prompt": "lamp", "faceCount": 30000,
			"credit_concept": map[string]interface{}{"blob": strings.Repeat("c", 4000)},
			"credit_main":    map[string]interface{}{"blob": strings.Repeat("m", 4000)},
			"text2geometry": map[string]interface{}{
				"method": "POST", "path": "/api/game3d/text2geometry", "status": 200, "error": "",
				"body": map[string]interface{}{"worksId": "wid-9", "requestId": "req-1", "model": ""},
			},
		}
	})

	result := callTool(t, s, "1", "hunyuan_generate_concept",
		map[string]interface{}{"prompt": "木质小台灯", "kind": "geometry", "faceCount": 30000})
	text := toolText(t, result)
	if result["isError"] == true {
		t.Fatalf("submit reported an error: %s", text)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("submit output is not JSON: %v", err)
	}
	if out["requestId"] != "req-1" {
		t.Errorf("requestId missing: %v", out)
	}
	stage, ok := out["text2geometry"].(map[string]interface{})
	if !ok {
		t.Fatalf("stage summary missing: %v", out)
	}
	if stage["worksId"] != "wid-9" {
		t.Errorf("worksId must be hoisted for polling: %v", stage)
	}
	if out["kind"] != "geometry" {
		t.Errorf("kind echo missing: %v", out["kind"])
	}
	for _, noise := range []string{"credit_concept", "credit_main", "blob", "cccc"} {
		if strings.Contains(text, noise) {
			t.Errorf("submit response leaked %q (%d bytes)", noise, len(text))
		}
	}
	if len(text) > 1500 {
		t.Errorf("submit response still %d bytes", len(text))
	}
}

// 13. An unexpected upstream shape must still be visible, not silently dropped.
func TestMCPSubmitSummaryFallback(t *testing.T) {
	s := stubServer(t, func(r *http.Request) (int, map[string]interface{}) {
		return 200, map[string]interface{}{
			"requestId":      "req-2",
			"credit_concept": map[string]interface{}{"blob": "x"},
			"surpriseField":  "upstream changed shape",
		}
	})
	result := callTool(t, s, "1", "hunyuan_generate_main", map[string]interface{}{"prompt": "robot"})
	text := toolText(t, result)
	if !strings.Contains(text, "surpriseField") {
		t.Errorf("unrecognised payload was dropped: %s", text)
	}
	if strings.Contains(text, "credit_concept") {
		t.Errorf("fallback leaked credit noise: %s", text)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 14. An invalid style must be reported, not silently swapped for the default.
// (argEnum's silent fallback is right for scope/kind, wrong for style.)
func TestMCPStyleRejectsUnknownValue(t *testing.T) {
	var called bool
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) {
		called = true
		return 200, map[string]interface{}{}
	})

	res := callTool(t, s, "1", "hunyuan_generate_concept", map[string]interface{}{
		"prompt": "机器人", "style": "claymation",
	})
	if res["isError"] != true {
		t.Errorf("unknown style must fail loudly: %v", res)
	}
	if !strings.Contains(toolText(t, res), "claymation") {
		t.Errorf("error should echo the bad value: %s", toolText(t, res))
	}
	if called {
		t.Error("invalid style must be rejected before any upstream call")
	}

	// Every documented upstream style is accepted.
	for _, style := range []string{"hand_drawn", "q_version", "steampunk", "futuristic", "pixel", "low-poly"} {
		res := callTool(t, s, "2", "hunyuan_generate_concept", map[string]interface{}{
			"prompt": "机器人", "style": style,
		})
		if res["isError"] == true {
			t.Errorf("style %q rejected: %s", style, toolText(t, res))
		}
	}
}

// 15. Style enums in the catalogue must match what the upstream accepts.
func TestMCPStyleEnumsCoverUpstream(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })
	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := msgs[0]["result"].(map[string]interface{})["tools"].([]interface{})

	enumOf := func(tool, field string) []string {
		for _, raw := range tools {
			tt := raw.(map[string]interface{})
			if tt["name"] != tool {
				continue
			}
			props := tt["inputSchema"].(map[string]interface{})["properties"].(map[string]interface{})
			p, _ := props[field].(map[string]interface{})
			// schemas are compared after JSON decoding, so enum is []interface{}
			var vals []string
			switch v := p["enum"].(type) {
			case []string:
				vals = v
			case []interface{}:
				for _, x := range v {
					if str, ok := x.(string); ok {
						vals = append(vals, str)
					}
				}
			}
			return vals
		}
		t.Fatalf("tool %s not found", tool)
		return nil
	}

	conceptStyles := enumOf("hunyuan_generate_concept", "style")
	want := []string{"hand_drawn", "q_version", "steampunk", "futuristic", "pixel", "low-poly"}
	for _, w := range want {
		found := false
		for _, got := range conceptStyles {
			if got == w {
				found = true
			}
		}
		if !found {
			t.Errorf("concept style enum missing %q (upstream accepts it): %v", w, conceptStyles)
		}
	}
	for _, got := range conceptStyles {
		if got == "" {
			t.Error("empty string must not be an enum value: omit the parameter to get the default")
		}
	}
	if mainStyles := enumOf("hunyuan_generate_main", "style"); len(mainStyles) < 5 {
		t.Errorf("main style enum too small: %v", mainStyles)
	}
}

// 16. MCP must never expose account-level capabilities: an LLM client has no
// business reading the session, spending decisions aside, and definitely no
// business importing cookies or logging the account out.
func TestMCPExposesNoAccountTools(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) { return 200, nil })
	msgs := runFrames(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := msgs[0]["result"].(map[string]interface{})["tools"].([]interface{})

	banned := []string{
		"hunyuan_import_cookies", // injects a session
		"hunyuan_logout",         // destroys the session
		"hunyuan_credits",        // account quota
		"hunyuan_login_status",   // session/identity state
	}
	present := map[string]bool{}
	for _, raw := range tools {
		if n, ok := raw.(map[string]interface{})["name"].(string); ok {
			present[n] = true
		}
	}
	for _, b := range banned {
		if present[b] {
			t.Errorf("account-level tool %s must not be exposed over MCP", b)
		}
	}
	// Generation and read-only board tools must survive the removal.
	for _, keep := range []string{
		"hunyuan_health", "hunyuan_generate_concept", "hunyuan_generate_main",
		"hunyuan_texture", "hunyuan_assets", "hunyuan_works",
	} {
		if !present[keep] {
			t.Errorf("expected tool %s is missing", keep)
		}
	}
}

// 17. hunyuan_health must not describe the account (cookies / session state).
func TestMCPHealthHidesAccountState(t *testing.T) {
	s := stubServer(t, func(*http.Request) (int, map[string]interface{}) {
		return 200, map[string]interface{}{
			"service": "hunyuan3d-proxy", "status": "up", "upstream": DefaultUpstream,
			"mode": "main", "port": "8812", "signKey": "abc",
			"cookieCount": 3, "sessionLike": true, // must be dropped
		}
	})
	result := callTool(t, s, "1", "hunyuan_health", nil)
	text := toolText(t, result)
	for _, leaked := range []string{"cookieCount", "sessionLike"} {
		if strings.Contains(text, leaked) {
			t.Errorf("health leaked account state %q: %s", leaked, text)
		}
	}
	for _, want := range []string{"upstream", "mode"} {
		if !strings.Contains(text, want) {
			t.Errorf("health should still report %q: %s", want, text)
		}
	}
}
