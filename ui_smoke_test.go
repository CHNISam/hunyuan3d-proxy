package main

// Go-only smoke test for the local Web UI + HTTP surface.
// Replaces the former Playwright script (ui.test.mjs): every assertion that was
// a real browser check is now done against the route table via httptest, plus
// static assertions over the served HTML. No Node / browser required.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// newTestHandler wires the production route table (including CORS) against a
// throwaway data dir so the developer's real cookie jar is never touched.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("PROXY_QUIET", "1")
	cfg := Config{Port: "8812", DataDir: t.TempDir(), Upstream: DefaultUpstream}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return corsMiddleware(buildMux(srv, NewLoginService(srv)))
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not JSON (%d): %v\nbody=%s", rec.Code, err, rec.Body.String())
	}
	return env
}

func dataMap(t *testing.T, env map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := env["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("envelope has no data object: %v", env)
	}
	return d
}

// readWebFile reads a file from the served web/ tree, resolving relative to the
// package directory so `go test` works from any cwd.
func readWebFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// 1. Health endpoint reports the service and its upstream.
func TestHealthEndpoint(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/health = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env["ok"] != true {
		t.Fatalf("ok != true: %v", env)
	}
	d := dataMap(t, env)
	if d["service"] != "hunyuan3d-proxy" {
		t.Fatalf("service = %v", d["service"])
	}
	if d["status"] != "up" {
		t.Fatalf("status = %v", d["status"])
	}
	if d["upstream"] != DefaultUpstream {
		t.Fatalf("upstream = %v, want %s", d["upstream"], DefaultUpstream)
	}
	if _, ok := d["signKey"].(string); !ok {
		t.Fatalf("signKey missing/not a string: %v", d["signKey"])
	}
}

// 2. The console HTML loads and still contains the elements the UI script binds.
func TestWebConsoleMarkup(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / content-type = %q", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		`id="healthBadge"`, // auto-connect badge
		`id="assetGrid"`,   // resource board
		`id="loginTabs"`,
		`id="modeSwitch"`,
		`id="mcpSection"`,     // MCP panel
		`id="mcpStatusBadge"`, // connection-status contract
		`id="mcpToolList"`,
		"ui/app.js",
		"ui/app.css",
		"ui/qr.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index.html missing %s", want)
		}
	}
	// Removed legacy testers must not come back.
	for _, gone := range []string{`id="baseUrl"`, `id="btnHealth"`, `id="btnSend"`, "检测连接"} {
		if strings.Contains(body, gone) {
			t.Errorf("index.html still contains removed tester %s", gone)
		}
	}
}

// 3. Every static asset the console references is served.
func TestStaticAssetsServed(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{
		"/ui/app.css",
		"/ui/app.js",
		"/ui/qr.js",
		"/docs",
		"/openapi.json",
	} {
		rec := do(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (body=%s)", path, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s returned an empty body", path)
		}
	}
}

// 4. Non-API responses must not be cacheable: a fresh index.html paired with a
// stale app.js crashes the console.
func TestStaticNoStore(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{"/", "/ui/app.js", "/docs"} {
		if cc := do(t, h, http.MethodGet, path).Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("GET %s Cache-Control = %q, want no-store", path, cc)
		}
	}
}

// 5. OpenAPI contract stays browsable by Swagger UI.
func TestOpenAPIContract(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200", rec.Code)
	}
	var spec struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		t.Fatalf("openapi version = %q", spec.OpenAPI)
	}
	if len(spec.Paths) < 30 {
		t.Fatalf("openapi paths = %d, want >= 30", len(spec.Paths))
	}
	for _, p := range []string{"/api/health", "/api/login/status", "/api/text3d/assets"} {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("openapi.json missing documented path %s", p)
		}
	}
}

// 6. Login status never leaks the raw account id.
func TestLoginStatusRedaction(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/api/login/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/login/status = %d, want 200", rec.Code)
	}
	d := dataMap(t, decodeEnvelope(t, rec))
	if _, ok := d["hyTokenPresent"].(bool); !ok {
		t.Errorf("hyTokenPresent must stay a boolean flag, got %v", d["hyTokenPresent"])
	}
	uid, _ := d["innerUserId"].(string)
	if uid != "" && uid != "***" && !strings.Contains(uid, "…") {
		t.Fatalf("innerUserId not masked: %q", uid)
	}
	// An empty jar must not claim a session.
	if d["loggedIn"] != false {
		t.Errorf("fresh data dir reported loggedIn=%v", d["loggedIn"])
	}
}

// 7. Mode switching is the only piece of interactive state on the server.
func TestModeSwitch(t *testing.T) {
	h := newTestHandler(t)
	d := dataMap(t, decodeEnvelope(t, do(t, h, http.MethodGet, "/api/mode")))
	if d["mode"] != ModeMain {
		t.Fatalf("default mode = %v, want %s", d["mode"], ModeMain)
	}
	rec := do(t, h, http.MethodPost, "/api/mode?mode=concept")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/mode = %d, want 200", rec.Code)
	}
	if d := dataMap(t, decodeEnvelope(t, rec)); d["mode"] != ModeConcept {
		t.Fatalf("mode after switch = %v, want %s", d["mode"], ModeConcept)
	}
	d = dataMap(t, decodeEnvelope(t, do(t, h, http.MethodGet, "/api/mode")))
	if d["mode"] != ModeConcept {
		t.Fatalf("mode not persisted: %v", d["mode"])
	}
	if rec := do(t, h, http.MethodDelete, "/api/mode"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/mode = %d, want 405", rec.Code)
	}
}

// 8. CORS preflight still answers for browser callers.
func TestCORSPreflight(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodOptions, "/api/health")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/health = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Fatal("missing Access-Control-Allow-Origin")
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-Hy-Mode") {
		t.Fatalf("Allow-Headers = %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

// 9. Unknown paths fail closed with the documented JSON error.
func TestUnknownRouteReturnsJSON404(t *testing.T) {
	rec := do(t, newTestHandler(t), http.MethodGet, "/definitely-not-a-route")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route = %d, want 404", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env["ok"] != false {
		t.Fatalf("404 envelope ok=%v, want false", env["ok"])
	}
}

// The console merges two sources whose createdAt formats differ: main sends
// Unix seconds, concept sends ISO-8601. The frontend had its own copy of the
// sort (separate from the server's) that only understood numbers, so every
// concept task scored 0 and sank below every main task — a freshly textured
// model never appeared at the top of the board.
func TestAssetBoardSortsMixedTimestampFormats(t *testing.T) {
	appJS := readWebFile(t, "web/ui/app.js")

	// The frontend timestamp parser must handle ISO strings, not just numbers.
	if !strings.Contains(appJS, "Date.parse") {
		t.Error("frontend asset sort must parse ISO timestamps (main=Unix sec, concept=ISO-8601)")
	}
	if strings.Contains(appJS, "const n = parseFloat(v);\n      return Number.isFinite(n) ? n : 0;") {
		t.Error("frontend asset sort still only understands numbers")
	}

	// Texture outputs must be labelled and ordered ahead of intermediate files,
	// otherwise a finished PBR model is buried under preview images.
	for _, want := range []string{
		"texturePaintingRsp.glbUrl",
		"texturePaintingRsp.pbrNormalImageUrl",
		"ASSET_ORDER",
		"★贴图GLB",
	} {
		if !strings.Contains(appJS, want) {
			t.Errorf("frontend is missing texture-output handling: %s", want)
		}
	}
}
