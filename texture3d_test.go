package main

// Offline tests for the texture stage. The upstream call is stubbed, so these
// assert request shaping, validation and error semantics without spending
// credits or touching the network.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// textureTestServer builds a Server whose upstream calls are captured.
func textureTestServer(t *testing.T, capture *map[string]interface{}) *Server {
	t.Helper()
	t.Setenv("PROXY_QUIET", "1")
	cfg := Config{Port: "0", DataDir: t.TempDir(), Upstream: DefaultUpstream}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// stubTextureCalls swaps the upstream seams and returns a restore func.
func stubTextureCalls(t *testing.T, worksList string, submit func(string, map[string]interface{}) (interface{}, int, error)) func() {
	t.Helper()
	prevSubmit := textureCall
	prevWorks := textureInputsFromWorksCall
	textureCall = func(_ *Server, _ *http.Request, path string, payload map[string]interface{}) (interface{}, int, error) {
		return submit(path, payload)
	}
	textureInputsFromWorksCall = func(*Server, *http.Request) string { return worksList }
	return func() {
		textureCall = prevSubmit
		textureInputsFromWorksCall = prevWorks
	}
}

func postTexture(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/text3d/texture", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.HandleText3DTexture(rec, req)
	return rec
}

func textureErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, rec.Body.String())
	}
	if env.OK {
		t.Fatalf("expected failure, got ok=true: %s", rec.Body.String())
	}
	return env.Error
}

// 1. Missing inputs are rejected before any upstream call.
func TestTextureValidatesInputs(t *testing.T) {
	srv := textureTestServer(t, nil)

	for _, tc := range []struct{ name, body, want string }{
		{"empty", `{}`, "fbxUrl is required"},
		{"unknown worksId", `{"worksId":"does-not-exist"}`, "fbxUrl is required"},
		{"image without imageUrl", `{"fbxUrl":"https://cdn/x.fbx"}`, "imageUrl is required"},
		{"views without viewsImage", `{"fbxUrl":"https://cdn/x.fbx","method":"views"}`, "viewsImage is required"},
		{"bad method", `{"fbxUrl":"https://cdn/x.fbx","method":"zzz"}`, "unknown method"},
		{"text without prompt", `{"fbxUrl":"https://cdn/x.fbx","method":"text"}`, "prompt is required"},
	} {
		rec := postTexture(t, srv, tc.body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: status = %d, want 4xx", tc.name, rec.Code)
		}
		if got := textureErr(t, rec); !strings.Contains(got, tc.want) {
			t.Errorf("%s: error = %q, want to contain %q", tc.name, got, tc.want)
		}
	}
}

// 2. Text-to-texture must go through texture_edit_local with editType=1.
// It must NOT use text2texture_v31: that endpoint runs
// hunyuan-3d-texture-preprocess-v1.0, which returned HTTP 400 on all 7 attempts.
func TestTextureTextPathUsesEditLocal(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := textureTestServer(t, nil)
	restore := stubTextureCalls(t, "", func(path string, payload map[string]interface{}) (interface{}, int, error) {
		gotPath = path
		gotBody = payload
		return map[string]interface{}{"worksId": "w"}, 200, nil
	})
	defer restore()

	rec := postTexture(t, srv, `{"worksId":"geom-1","fbxUrl":"https://cdn/x.fbx","method":"text","prompt":"木质台灯"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("method=text status = %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(gotPath, "text2texture") {
		t.Errorf("text-to-texture must not use the broken text2texture endpoint: %s", gotPath)
	}
	if !strings.Contains(gotPath, "texture_edit_local") {
		t.Errorf("upstream path = %q, want texture_edit_local", gotPath)
	}
	if gotBody["editType"] != 1 {
		t.Errorf("editType = %v, want 1 (TEXT_2_TEXTURE)", gotBody["editType"])
	}
	if gotBody["prompt"] != "木质台灯" {
		t.Errorf("prompt not forwarded: %v", gotBody["prompt"])
	}
	if _, ok := gotBody["localNames"]; !ok {
		t.Error("localNames must be present (empty slice = whole model)")
	}
	// An empty prompt cannot drive a text texture.
	rec = postTexture(t, srv, `{"worksId":"geom-1","fbxUrl":"https://cdn/x.fbx","method":"text"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty prompt status = %d, want 400", rec.Code)
	}
}

// 3. The geometry fbx/image are derived from worksId automatically, so a
// caller only needs the worksId of the model they want textured.
func TestTextureDerivesInputsFromWorks(t *testing.T) {
	var sawPath string
	var gotBody map[string]interface{}

	worksList := `{"data":{"list":[{"worksId":"geom-1","modelInfo":{` +
		`"geometryGenerationRsp":{"fbxUrl":"https://cdn/model.fbx","imageUrl":"https://cdn/preview.png"}` +
		`}}]}}`

	srv := textureTestServer(t, nil)
	restore := stubTextureCalls(t, worksList, func(path string, payload map[string]interface{}) (interface{}, int, error) {
		sawPath = path
		gotBody = payload
		return map[string]interface{}{"worksId": "new-tex-1", "requestId": "goX"}, 200, nil
	})
	defer restore()

	rec := postTexture(t, srv, `{"worksId":"geom-1","method":"image"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(sawPath, "image2texture_v31") {
		t.Errorf("upstream path = %q", sawPath)
	}
	if gotBody["fbxUrl"] != "https://cdn/model.fbx" {
		t.Errorf("fbxUrl not derived: %v", gotBody["fbxUrl"])
	}
	if gotBody["imageUrl"] != "https://cdn/preview.png" {
		t.Errorf("imageUrl not derived: %v", gotBody["imageUrl"])
	}
	if gotBody["dependOnWorksId"] != "geom-1" {
		t.Errorf("dependOnWorksId = %v", gotBody["dependOnWorksId"])
	}
	// The three fields the upstream requires (absent from the docs) must be present.
	for _, k := range []string{"worksType", "requestId", "modelVersion"} {
		if _, ok := gotBody[k]; !ok {
			t.Errorf("required upstream field %q missing from payload", k)
		}
	}
	if gotBody["worksType"].(int) != 2 {
		t.Errorf("worksType = %v, want 2", gotBody["worksType"])
	}
	// The new worksId must come back so the caller can poll it.
	if !strings.Contains(rec.Body.String(), "new-tex-1") {
		t.Errorf("response omits the new worksId: %s", rec.Body.String())
	}
}

// 4. keepUv is forwarded verbatim: it changes whether the geometry's UV layout
// is preserved, so a silent default would alter the result.
func TestTextureForwardsKeepUv(t *testing.T) {
	for _, want := range []bool{true, false} {
		var got map[string]interface{}
		srv := textureTestServer(t, nil)
		restore := stubTextureCalls(t, "", func(path string, payload map[string]interface{}) (interface{}, int, error) {
			got = payload
			return map[string]interface{}{"worksId": "w"}, 200, nil
		})
		defer restore()
		body := `{"fbxUrl":"https://cdn/x.fbx","imageUrl":"https://cdn/p.png"}`
		if want {
			body = `{"fbxUrl":"https://cdn/x.fbx","imageUrl":"https://cdn/p.png","keepUv":true}`
		}
		postTexture(t, srv, body)
		if got["keepUv"] != want {
			t.Errorf("keepUv = %v, want %v", got["keepUv"], want)
		}
	}
}

// 5. The views path targets the multi-view endpoint.
func TestTextureViewsPath(t *testing.T) {
	var sawPath string
	srv := textureTestServer(t, nil)
	restore := stubTextureCalls(t, "", func(path string, payload map[string]interface{}) (interface{}, int, error) {
		sawPath = path
		return map[string]interface{}{"worksId": "w"}, 200, nil
	})
	defer restore()
	rec := postTexture(t, srv, `{"fbxUrl":"https://cdn/x.fbx","viewsImage":"https://cdn/views.png","method":"views"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(sawPath, "views2texture") {
		t.Errorf("path = %q, want views2texture", sawPath)
	}
}

// 6. A GET is refused (this endpoint spends credits).
func TestTextureRejectsGET(t *testing.T) {
	srv := textureTestServer(t, nil)
	rec := httptest.NewRecorder()
	srv.HandleText3DTexture(rec, httptest.NewRequest(http.MethodGet, "/api/text3d/texture", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}
}

// 7. PBR is ON by default: a model without PBR maps has no material at all.
// The mode=main branch used to hardcode enable_pbr=false while the dedicated
// main handler sent true — the same request behaved differently by entry point.
func TestMainPBREnabledByDefault(t *testing.T) {
	// The opt-out must exist and default to false (i.e. PBR enabled).
	var req text3dRequest
	if req.DisablePBR {
		t.Fatal("DisablePBR must default to false so PBR is on")
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "disablePbr") {
		t.Errorf("the zero value must not serialise an opt-out: %s", raw)
	}
	// And it must be honoured when set.
	off := text3dRequest{DisablePBR: true}
	raw, _ = json.Marshal(off)
	if !strings.Contains(string(raw), `"disablePbr":true`) {
		t.Errorf("DisablePBR did not serialise: %s", raw)
	}

	// Both main-site entry points must compute enable_pbr from the same field,
	// so a request cannot mean two different things.
	src := readWebFile(t, "text3d.go")
	if strings.Contains(src, `"enable_pbr":    false,`) {
		t.Error("enable_pbr is still hardcoded false for one entry point")
	}
}

// 8. method=text must feed the upstream a UV-unwrapped mesh. Passing the raw
// geometry fbx makes the upstream run and complete without emitting any
// texturePaintingRsp outputs — verified live. So when a task already has a
// texture stage, its fbx wins over the geometry mesh.
func TestTextureTextPrefersUnwrappedFbx(t *testing.T) {
	const works = `{"data":{"list":[{"worksId":"w1","modelInfo":{` +
		`"geometryGenerationRsp":{"fbxUrl":"https://cdn/raw-geometry.fbx","imageUrl":"https://cdn/geo.png"},` +
		`"texturePaintingRsp":{"fbxUrl":"https://cdn/unwrapped.fbx","imageUrl":"https://cdn/tex.png"}` +
		`}}]}}`

	cases := []struct {
		method   string
		wantFbx  string
		wantNote string
	}{
		{"text", "https://cdn/unwrapped.fbx", "text needs the UV-unwrapped mesh"},
		{"image", "https://cdn/raw-geometry.fbx", "image path uses the geometry mesh"},
	}
	for _, tc := range cases {
		var got map[string]interface{}
		srv := textureTestServer(t, nil)
		restore := stubTextureCalls(t, works, func(path string, payload map[string]interface{}) (interface{}, int, error) {
			got = payload
			return map[string]interface{}{"worksId": "w"}, 200, nil
		})
		body := `{"worksId":"w1","method":"` + tc.method + `"}`
		if tc.method == "text" {
			body = `{"worksId":"w1","method":"text","prompt":"木质"}`
		}
		rec := postTexture(t, srv, body)
		restore()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d (%s)", tc.method, rec.Code, rec.Body.String())
		}
		if got["fbxUrl"] != tc.wantFbx {
			t.Errorf("%s: fbxUrl = %v, want %v (%s)", tc.method, got["fbxUrl"], tc.wantFbx, tc.wantNote)
		}
	}
}

// 9. The board must expose which upstream endpoint a task came from, and the
// model chain, because the upstream's reqPath records the LAST stage rather
// than the endpoint that was submitted (a texture_edit_local job reports
// image2texture_v31). Showing both is the only way to tell them apart.
func TestAssetItemsCarryUpstreamEndpoint(t *testing.T) {
	// The data path: AssetItem must serialise both fields.
	item := AssetItem{
		Source:          "concept",
		ID:              "w1",
		Status:          "success",
		UpstreamRequest: "/api/game3d/texture_painting/texture_edit_local",
		ModelChain:      "hunyuan-3d-images-subject-segmentation;hunyuan-3d-texture-edit-text-v1.0;hunyuan-3d-image2texture-v3.1",
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{"upstreamRequest", "modelChain", "texture_edit_local", "texture-edit-text"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("AssetItem JSON missing %q: %s", want, raw)
		}
	}

	// The UI must render them (otherwise the field is dead weight).
	appJS := readWebFile(t, "web/ui/app.js")
	for _, want := range []string{"asset-upstream", "upstreamRequest", "modelChain"} {
		if !strings.Contains(appJS, want) {
			t.Errorf("asset board does not render %q", want)
		}
	}
	// The stale hardcoded endpoint list must know about the texture routes.
	src := readWebFile(t, "assets.go")
	for _, want := range []string{"textureText", "textureImage", "texture_edit_local"} {
		if !strings.Contains(src, want) {
			t.Errorf("assets.go endpoint map missing %q", want)
		}
	}
}
