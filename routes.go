package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// route is a registered fixed proxy route.
type route struct {
	method  string // "" = any
	mode    string // main | concept
	path    string // local path (exact) — for documentation
	handler http.HandlerFunc
}

// buildMux registers every local API route on mux.
func buildMux(s *Server, login *LoginService) http.Handler {
	mux := http.NewServeMux()

	// ---------- meta ----------
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, map[string]interface{}{
			"service":     "hunyuan3d-proxy",
			"status":      "up",
			"upstream":    s.Cfg.Upstream,
			"mode":        s.DefaultMode(),
			"port":        s.Cfg.Port,
			"dataDir":     s.Cfg.DataDir,
			"cookieCount": s.Jar.Len(),
			"sessionLike": s.Jar.HasSessionHeuristic(),
			"signKey":     string(SignKey()),
			"signKeyHex":  SignatureDebug()["key_hex"],
		})
	})

	mux.HandleFunc("/api/mode", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeOK(w, map[string]interface{}{
				"mode":     s.DefaultMode(),
				"upstream": s.Cfg.Upstream,
				"options":  []string{ModeMain, ModeConcept},
			})
		case http.MethodPost, http.MethodPut:
			q := r.URL.Query().Get("mode")
			if q == "" {
				q = r.Header.Get("X-Hy-Mode")
			}
			if q == "" {
				q = r.FormValue("mode")
			}
			if q == "" && r.Body != nil {
				// try tiny JSON {"mode":"concept"}
				var payload struct {
					Mode string `json:"mode"`
				}
				if err := readJSONBody(r, &payload); err == nil && payload.Mode != "" {
					q = payload.Mode
				}
			}
			if q == "" {
				writeErr(w, http.StatusBadRequest, "provide ?mode=main|concept or body {\"mode\":\"...\"}")
				return
			}
			if !IsValidMode(q) && NormalizeMode(q) != strings.ToLower(q) {
				// still normalize
			}
			m := s.SetDefaultMode(q)
			writeOK(w, map[string]interface{}{"mode": m})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "GET or POST")
		}
	})

	mux.HandleFunc("/api/sign/debug", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, SignatureDebug())
	})

	// ---------- login ----------
	mux.HandleFunc("/api/login/qr/start", login.HandleQRStart)
	mux.HandleFunc("/api/login/qr/status", login.HandleQRStatus)
	mux.HandleFunc("/api/login/qr/image", login.HandleQRImage)
	mux.HandleFunc("/api/login/status", login.HandleStatus)
	mux.HandleFunc("/api/login/logout", login.HandleLogout)
	mux.HandleFunc("/api/login/cookies", login.HandleImportCookies)
	mux.HandleFunc("/api/login/providers", login.HandleProviders)
	mux.HandleFunc("/api/login/oauth/callback", login.HandleOAuthCallback)
	mux.HandleFunc("/api/login/email/send-code", login.HandleEmailSendCode)
	mux.HandleFunc("/api/login/email/verify", login.HandleEmailVerify)
	mux.HandleFunc("/api/login/phone/send-code", login.HandlePhoneSendCode)
	mux.HandleFunc("/api/login/phone/verify", login.HandlePhoneVerify)

	// login-related upstream proxies
	mux.HandleFunc("/api/jointappid", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyMain(w, r, "/api/jointappid")
	})
	mux.HandleFunc("/api/open/auth", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyMain(w, r, "/api/open/auth")
	})
	// auth-center catch-all: /api/auth-center/*
	mux.HandleFunc("/api/auth-center/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		s.ProxyMain(w, r, path)
	})

	// ---------- credits (unified) ----------
	mux.HandleFunc("/api/credit/", func(w http.ResponseWriter, r *http.Request) {
		which := strings.TrimPrefix(r.URL.Path, "/api/credit/")
		which = strings.Trim(which, "/")
		s.HandleCredit(w, r, which)
	})

	// ---------- text-to-3D helpers (Go-side wrappers) ----------
	mux.HandleFunc("/api/text3d/generate", s.HandleText3DGenerate)
	mux.HandleFunc("/api/text3d/works", s.HandleText3DWorks)
	mux.HandleFunc("/api/text3d/main", s.HandleMainText3D)
	mux.HandleFunc("/api/text3d/main/list", s.HandleMainCreationsList)
	mux.HandleFunc("/api/text3d/assets", s.HandleAssetBoard)
	mux.HandleFunc("/api/text3d/texture", s.HandleText3DTexture)

	// ---------- official Tencent Cloud ai3d API 3.0 ----------
	// Docs: https://cloud.tencent.com/document/product/1804/120829
	// GET  /api/cloud
	// POST /api/cloud/submit-pro | query-pro | submit-rapid | ...
	// POST /api/cloud/action/SubmitHunyuanTo3DProJob
	mux.HandleFunc("/api/cloud", s.HandleCloud)
	mux.HandleFunc("/api/cloud/", s.HandleCloud)

	// ---------- MAIN fixed routes ----------
	mainRoutes := []struct {
		method string
		local  string // under /api/...
		up     string // upstream path
	}{
		{http.MethodGet, "/api/3d/config", "/api/3d/config"},
		{http.MethodPost, "/api/3d/resource/genUploadInfo", "/api/3d/resource/genUploadInfo"},
		{http.MethodPost, "/api/3d/creations/generations", "/api/3d/creations/generations"},
		{http.MethodGet, "/api/3d/creations/detail", "/api/3d/creations/detail"},
		{http.MethodPost, "/api/3d/resource/review", "/api/3d/resource/review"},
		{http.MethodPost, "/api/3d/resource/cmykCheck", "/api/3d/resource/cmykCheck"},
		{http.MethodPost, "/api/3d/creations/boneBindingFromMesh", "/api/3d/creations/boneBindingFromMesh"},
		{http.MethodPost, "/api/3d/creations/manualBoneBindingFromMesh", "/api/3d/creations/manualBoneBindingFromMesh"},
		{http.MethodPost, "/api/3d/creations/manualBoneBinding", "/api/3d/creations/manualBoneBinding"},
		{http.MethodPost, "/api/3d/creations/cancel", "/api/3d/creations/cancel"},
		{http.MethodPost, "/api/3d/creations/count", "/api/3d/creations/count"},
		{http.MethodPost, "/api/3d/creations/list", "/api/3d/creations/list"},
		{http.MethodPost, "/api/3d/creations/retopologize", "/api/3d/creations/retopologize"},
		{http.MethodPost, "/api/3d/creations/delete", "/api/3d/creations/delete"},
		{http.MethodPost, "/api/3d/creations/resourceConvert", "/api/3d/creations/resourceConvert"},
		{http.MethodPost, "/api/3d/feedback/submit", "/api/3d/feedback/submit"},
		{http.MethodPost, "/api/3d/quotainfo", "/api/3d/quotainfo"},
		{http.MethodGet, "/api/3d/planCreditInfo", "/api/3d/planCreditInfo"},
		{http.MethodGet, "/api/3d/purchaseList", "/api/3d/purchaseList"},
		{http.MethodPost, "/api/3d/purchaseBasicPlan", "/api/3d/purchaseBasicPlan"},
		{http.MethodGet, "/api/3d/interaction/config", "/api/3d/interaction/config"},
		{http.MethodGet, "/api/3d/worldPlay/config", "/api/3d/worldPlay/config"},
		{http.MethodPost, "/api/3d/notice/list", "/api/3d/notice/list"},
		{http.MethodPost, "/api/3d/notice/statusUpdate", "/api/3d/notice/statusUpdate"},
		{http.MethodPost, "/api/3d/worldPlay/text2Image", "/api/3d/worldPlay/text2Image"},
		{http.MethodPost, "/api/3d/worldPlay/page2video", "/api/3d/worldPlay/page2video"},
		{http.MethodPost, "/api/3d/worldScene/review", "/api/3d/worldScene/review"},
		// /api/open/auth is registered above under login-related proxies.
	}
	for _, rt := range mainRoutes {
		local, up := rt.local, rt.up
		method := rt.method
		mux.HandleFunc(local, methodGuard(method, func(w http.ResponseWriter, r *http.Request) {
			// Credit passthrough still uses transparent proxy (raw upstream body
			// is available via reverse proxy); envelope debug via /api/credit/*.
			s.ProxyMain(w, r, up)
		}))
	}

	// ---------- CONCEPT fixed routes ----------
	// Aliases: /3d/getuserinfo and /api/concept/getuserinfo
	mux.HandleFunc("/3d/getuserinfo", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyConcept(w, r, "/3d/getuserinfo")
	})
	mux.HandleFunc("/api/concept/getuserinfo", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyConcept(w, r, "/3d/getuserinfo")
	})

	conceptRoutes := []struct {
		method string
		local  string
		up     string
	}{
		{http.MethodPost, "/game3d/general_info/get_remaining_times", "/game3d/general_info/get_remaining_times"},
		{http.MethodPost, "/game3d/general_info/get_works_list", "/game3d/general_info/get_works_list"},
		{http.MethodPost, "/game3d/general_info/delete_works", "/game3d/general_info/delete_works"},
		{http.MethodPost, "/game3d/general_info/regenerate_work", "/game3d/general_info/regenerate_work"},
		{http.MethodPost, "/game3d/general_info/get_good_case", "/game3d/general_info/get_good_case"},
		{http.MethodPost, "/game3d/general_info/get_motion_case", "/game3d/general_info/get_motion_case"},
		{http.MethodPost, "/game3d/resource/upload", "/game3d/resource/upload"},
		{http.MethodPost, "/game3d/resource/download", "/game3d/resource/download"},
		{http.MethodPost, "/game3d/resource/character_validations", "/game3d/resource/character_validations"},
		{http.MethodPost, "/game3d/resource/format_conversions", "/game3d/resource/format_conversions"},
		{http.MethodPost, "/game3d/share/create", "/game3d/share/create"},
		{http.MethodGet, "/game3d/share/detail", "/game3d/share/detail"},
		{http.MethodPost, "/game3d/feedback/like", "/game3d/feedback/like"},
		{http.MethodPost, "/game3d/feedback/dislike", "/game3d/feedback/dislike"},
		{http.MethodPost, "/game3d/feedback/cancel", "/game3d/feedback/cancel"},
		{http.MethodPost, "/game3d/feedback/get", "/game3d/feedback/get"},
		{http.MethodPost, "/game3d/feedback/get_dislike_reasons", "/game3d/feedback/get_dislike_reasons"},
		{http.MethodPost, "/game3d/component_splitting/v2/part_split", "/game3d/component_splitting/v2/part_split"},
		{http.MethodPost, "/game3d/component_splitting/v2/pre_segmentation", "/game3d/component_splitting/v2/pre_segmentation"},
		{http.MethodPost, "/game3d/component_splitting/v2/smooth_segmentation", "/game3d/component_splitting/v2/smooth_segmentation"},
		{http.MethodPost, "/game3d/component_splitting/part_split", "/game3d/component_splitting/part_split"},
		{http.MethodPost, "/game3d/component_splitting/part_merge", "/game3d/component_splitting/part_merge"},
		{http.MethodPost, "/game3d/component_splitting/part_adjust", "/game3d/component_splitting/part_adjust"},
		{http.MethodPost, "/game3d/component_splitting/update_component_splitting", "/game3d/component_splitting/update_component_splitting"},
		{http.MethodPost, "/new-portal/whitelist/apply", "/new-portal/whitelist/apply"},
		{http.MethodPost, "/new-portal/whitelist/query", "/new-portal/whitelist/query"},

		// concept generation family
		{http.MethodPost, "/game3d/concept_design/image2views", "/game3d/concept_design/image2views"},
		{http.MethodPost, "/game3d/concept_design/text2image", "/game3d/concept_design/text2image"},
		{http.MethodPost, "/game3d/bone_skinning/bone_skinning", "/game3d/bone_skinning/bone_skinning"},
		{http.MethodPost, "/game3d/uv_unwrapping/uv_unwrapping", "/game3d/uv_unwrapping/uv_unwrapping"},
		{http.MethodPost, "/game3d/low_poly_topology/image2low_poly", "/game3d/low_poly_topology/image2low_poly"},
		{http.MethodPost, "/game3d/low_poly_topology/low_poly_topology", "/game3d/low_poly_topology/low_poly_topology"},
		{http.MethodPost, "/game3d/motion_generation/text2motion", "/game3d/motion_generation/text2motion"},
		{http.MethodPost, "/game3d/motion_generation/video2motion", "/game3d/motion_generation/video2motion"},
		{http.MethodPost, "/game3d/motion_retarget/motion_retarget", "/game3d/motion_retarget/motion_retarget"},
		{http.MethodPost, "/game3d/texture_painting/image2texture_v31", "/game3d/texture_painting/image2texture_v31"},
		{http.MethodPost, "/game3d/texture_painting/image2texture_v35preview", "/game3d/texture_painting/image2texture_v35preview"},
		{http.MethodPost, "/game3d/texture_painting/text2texture", "/game3d/texture_painting/text2texture"},
		{http.MethodPost, "/game3d/texture_painting/text2texture_v31", "/game3d/texture_painting/text2texture_v31"},
		{http.MethodPost, "/game3d/texture_painting/texture_edit_local", "/game3d/texture_painting/texture_edit_local"},
		{http.MethodPost, "/game3d/texture_painting/texture_segmentation", "/game3d/texture_painting/texture_segmentation"},
		{http.MethodPost, "/game3d/texture_painting/update_texture_painting", "/game3d/texture_painting/update_texture_painting"},
		{http.MethodPost, "/game3d/texture_painting/views2texture", "/game3d/texture_painting/views2texture"},
		{http.MethodPost, "/game3d/texture_painting/views2texture_v35preview", "/game3d/texture_painting/views2texture_v35preview"},

		// geometry generation
		{http.MethodPost, "/game3d/geometry_generation/image2geometry", "/game3d/geometry_generation/image2geometry"},
		{http.MethodPost, "/game3d/geometry_generation/views2geometry", "/game3d/geometry_generation/views2geometry"},
		{http.MethodPost, "/game3d/geometry_generation/5views2geometry", "/game3d/geometry_generation/5views2geometry"},
		{http.MethodPost, "/game3d/geometry_generation/text2geometry", "/game3d/geometry_generation/text2geometry"},
		{http.MethodPost, "/game3d/geometry_generation/image2geometry_singleimage", "/game3d/geometry_generation/image2geometry_singleimage"},
		{http.MethodPost, "/game3d/geometry_generation/image2geometry_views", "/game3d/geometry_generation/image2geometry_views"},
	}
	for _, rt := range conceptRoutes {
		local, up := rt.local, rt.up
		method := rt.method
		mux.HandleFunc(local, methodGuard(method, func(w http.ResponseWriter, r *http.Request) {
			s.ProxyConcept(w, r, up)
		}))
	}

	// Also mount concept routes under /api/concept/ prefix for discovery-friendly clients.
	for _, rt := range conceptRoutes {
		up := rt.up
		method := rt.method
		local := "/api/concept" + rt.up
		mux.HandleFunc(local, methodGuard(method, func(w http.ResponseWriter, r *http.Request) {
			s.ProxyConcept(w, r, up)
		}))
	}

	// ---------- generic catch-all proxies ----------
	// ANY /api/main/{path...}  -> upstream /{path...}  (mode=main)
	mux.HandleFunc("/api/main/", func(w http.ResponseWriter, r *http.Request) {
		path := catchAll(r.URL.Path, "/api/main")
		s.ProxyMain(w, r, path)
	})
	// ANY /api/concept/{path...} -> upstream /{path...} (mode=concept)
	// NOTE: fixed /api/concept/getuserinfo is registered above and wins (ServeMux longest prefix).
	mux.HandleFunc("/api/concept/", func(w http.ResponseWriter, r *http.Request) {
		path := catchAll(r.URL.Path, "/api/concept")
		// /api/concept/getuserinfo already handled; other paths:
		s.ProxyConcept(w, r, path)
	})
	// ANY /proxy/{mode}/{path...}
	mux.HandleFunc("/proxy/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/proxy/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			writeErr(w, http.StatusBadRequest, "usage: /proxy/{main|concept}/{upstream/path}")
			return
		}
		mode := NormalizeMode(parts[0])
		up := "/"
		if len(parts) > 1 {
			up = "/" + parts[1]
		}
		s.ProxyMode(w, r, mode, up)
	})

	// ---------- catch-all for /game3d/ and /new-portal/ (unlisted concept APIs) ----------
	mux.HandleFunc("/game3d/", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyConcept(w, r, r.URL.Path)
	})
	mux.HandleFunc("/new-portal/", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyConcept(w, r, r.URL.Path)
	})
	// /3d/* catch-all (concept user info family)
	mux.HandleFunc("/3d/", func(w http.ResponseWriter, r *http.Request) {
		s.ProxyConcept(w, r, r.URL.Path)
	})

	// index + Web UI static files
	uiDirs := []string{
		"web",
		".",
		"..",
		filepath.Join("..", "ui"),
		"ui",
	}
	// Prefer workspace copies used by the desktop preview.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		uiDirs = append([]string{
			filepath.Join(dir, "web"),
			filepath.Join(dir, ".."),
			filepath.Join(dir, "..", "ui"),
			"D:/lose",
			"D:/lose/ui",
		}, uiDirs...)
	}

	findUI := func(name string) string {
		for _, d := range uiDirs {
			p := filepath.Join(d, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
		return ""
	}

	// ---------- API Doc (OpenAPI + Swagger UI) ----------
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		if p := findUI(filepath.Join("web", "openapi.json")); p != "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			http.ServeFile(w, r, p)
			return
		}
		if p := findUI("openapi.json"); p != "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			http.ServeFile(w, r, p)
			return
		}
		writeErr(w, http.StatusNotFound, "openapi.json not found next to binary or in web/")
	})
	serveDocs := func(w http.ResponseWriter, r *http.Request) {
		if p := findUI(filepath.Join("web", "docs.html")); p != "" {
			http.ServeFile(w, r, p)
			return
		}
		if p := findUI("docs.html"); p != "" {
			http.ServeFile(w, r, p)
			return
		}
		writeErr(w, http.StatusNotFound, "docs.html not found next to binary or in web/")
	}
	mux.HandleFunc("/docs", serveDocs)
	mux.HandleFunc("/docs/", serveDocs)

	mux.HandleFunc("/ui/", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/ui/")
		if rel == "" || rel == "index.html" {
			// serve workspace index for /ui/ as well
			if p := findUI("index.html"); p != "" {
				http.ServeFile(w, r, p)
				return
			}
			rel = "index.html"
		}
		// allow /ui/app.js → ui/app.js or web/ui/app.js
		candidates := []string{
			filepath.Join("ui", filepath.ToSlash(rel)),
			filepath.Join("web", "ui", filepath.ToSlash(rel)),
			filepath.Join("D:/lose/ui", filepath.ToSlash(rel)),
			rel,
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				http.ServeFile(w, r, c)
				return
			}
		}
		// also try D:/lose/index.html for empty-ish
		if p := findUI("index.html"); p != "" && (rel == "index.html") {
			http.ServeFile(w, r, p)
			return
		}
		writeErr(w, http.StatusNotFound, "ui asset not found: "+rel)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" && path != "/index.html" && path != "/ui" && path != "/ui/" {
			// static asset next to index.html (ui/app.css etc already under /ui/)
			if f := findUI(strings.TrimPrefix(path, "/")); f != "" {
				http.ServeFile(w, r, f)
				return
			}
			writeErr(w, http.StatusNotFound, "no route for "+path+"; see GET /api/health, Web UI at / or /ui/")
			return
		}
		// Serve Web UI if present, else JSON service index.
		for _, name := range []string{"index.html", filepath.Join("web", "index.html")} {
			if p := findUI(name); p != "" {
				http.ServeFile(w, r, p)
				return
			}
		}
		writeOK(w, map[string]interface{}{
			"service":  "hunyuan3d-proxy",
			"ui":       "place index.html + ui/ next to the binary or at D:/lose",
			"docs":     []string{"/docs", "/openapi.json", "/api/health", "/api/mode", "/api/login/status", "/api/credit/all", "/api/cloud"},
			"modes":    []string{ModeMain, ModeConcept},
			"upstream": s.Cfg.Upstream,
		})
	})

	// ---------- MCP over HTTP ----------
	// The in-process handler this MCP server dispatches tools through is the
	// mux being built right now, so wire it lazily: buildMux fills this holder
	// in before it returns, and requests only arrive after that.
	var selfHandler http.Handler
	mcpStats := &mcpHTTPStats{}
	dispatch := func(method, path string, body []byte) (*localResult, error) {
		return inProcessCall(selfHandler, method, path, body)
	}
	mcpHTTP := newMCPServer(mcpToolSet(dispatch), dispatch)
	mux.HandleFunc("/mcp", s.HandleMCP(mcpHTTP, mcpStats))
	mux.HandleFunc("/api/mcp/panel", s.HandleMCPPanel(mcpHTTP, mcpStats))

	handler := noStoreStatic(mux)
	selfHandler = handler
	return handler
}

// noStoreStatic forces revalidation on non-API paths. Local UI files change
// often; heuristic caching (Last-Modified only) lets the browser pair a fresh
// index.html with a stale app.js, which then crashes on removed elements.
func noStoreStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func methodGuard(method string, next http.HandlerFunc) http.HandlerFunc {
	if method == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method && r.Method != http.MethodOptions {
			writeErr(w, http.StatusMethodNotAllowed, method+" required")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}
