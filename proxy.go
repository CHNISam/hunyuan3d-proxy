package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Server wires config, cookie jar, default mode, and reverse proxies.
type Server struct {
	Cfg Config
	Jar *Jar

	mu          sync.RWMutex
	defaultMode string

	mainProxy    *httputil.ReverseProxy
	conceptProxy *httputil.ReverseProxy
	upstreamURL  *url.URL
}

// NewServer builds proxies and loads the cookie jar.
func NewServer(cfg Config) (*Server, error) {
	up, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, err
	}
	jarPath := cfg.DataDir + "/cookies.json"
	// normalize windows path separators via Join in caller; keep simple
	jar := NewJar(jarPath)
	s := &Server{
		Cfg:          cfg,
		Jar:          jar,
		defaultMode:  ModeMain,
		upstreamURL:  up,
		mainProxy:    newProxy(up, ModeMain, jar),
		conceptProxy: newProxy(up, ModeConcept, jar),
	}
	return s, nil
}

// DefaultMode returns the global default mode.
func (s *Server) DefaultMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultMode
}

// SetDefaultMode sets the global default mode.
func (s *Server) SetDefaultMode(mode string) string {
	mode = NormalizeMode(mode)
	s.mu.Lock()
	s.defaultMode = mode
	s.mu.Unlock()
	return mode
}

// ResolveMode picks the effective mode for a request:
// 1. header X-Hy-Mode
// 2. query ?mode=
// 3. route-implied mode (passed in as hint)
// 4. global default
func (s *Server) ResolveMode(r *http.Request, routeHint string) string {
	if h := r.Header.Get("X-Hy-Mode"); h != "" {
		return NormalizeMode(h)
	}
	if q := r.URL.Query().Get("mode"); q != "" {
		return NormalizeMode(q)
	}
	if routeHint != "" {
		return NormalizeMode(routeHint)
	}
	return s.DefaultMode()
}

func newProxy(target *url.URL, mode string, jar *Jar) *httputil.ReverseProxy {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// Keep path as rewritten by the handler (req.URL.Path already set).
			if req.URL.RawQuery == "" {
				// keep
			}
			injectHeaders(req, mode, jar)
		},
		ModifyResponse: func(resp *http.Response) error {
			jar.AbsorbResponseCookies(resp.Header, target)
			// Strip hop-by-hop leftovers already handled by ReverseProxy.
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeJSON(w, http.StatusBadGateway, APIEnvelope{
				OK:    false,
				Error: "upstream error: " + err.Error(),
				Mode:  mode,
			})
		},
	}
	return rp
}

// injectHeaders adds the browser/web headers Hunyuan3D expects and
// attaches jar cookies (withCredentials equivalent).
func injectHeaders(req *http.Request, mode string, jar *Jar) {
	req.Header.Set("User-Agent", DefaultUA)
	req.Header.Set("Origin", "https://"+UpstreamHost)
	req.Header.Set("Referer", RefererFor(mode))
	req.Header.Set("x-source", "web")
	req.Header.Set("x-product", "hunyuan3d")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if req.Body != nil && req.Body != http.NoBody {
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	// Forward / overlay cookies from the jar. Incoming client cookies are
	// kept if the jar does not define the same name.
	jarCookie := jar.CookieHeader()
	clientCookie := req.Header.Get("Cookie")
	merged := mergeCookieHeaders(clientCookie, jarCookie)
	if merged != "" {
		req.Header.Set("Cookie", merged)
	} else {
		req.Header.Del("Cookie")
	}
	// x_hunyuan_inner_user_id = value of hy_user (also mirrored as m.Iq).
	withInnerUser(req, jar)
}

func jarValue(jar *Jar, name string) string {
	for _, c := range jar.Snapshot() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// mergeCookieHeaders merges two Cookie header values; b overrides a on name clash.
func mergeCookieHeaders(a, b string) string {
	type kv struct{ k, v string }
	order := []string{}
	seen := map[string]string{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		parts := strings.Split(s, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			eq := strings.Index(p, "=")
			if eq <= 0 {
				continue
			}
			k := strings.TrimSpace(p[:eq])
			v := strings.TrimSpace(p[eq+1:])
			if _, ok := seen[k]; !ok {
				order = append(order, k)
			}
			seen[k] = v
		}
	}
	add(a)
	add(b)
	if len(order) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, k := range order {
		if i > 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(k)
		buf.WriteString("=")
		buf.WriteString(seen[k])
	}
	return buf.String()
}

// stripPrefix removes prefix from path (with or without leading slash variants).
func stripPrefix(path, prefix string) string {
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix)
	}
	return path
}

// conceptUpstreamPath maps local concept convenience paths to real upstream API paths.
// Discovery: game3d / new-portal / 3d APIs live under /api/... on the host
// (bare /game3d/* returns nginx 405; /3d/* returns SPA HTML).
func conceptUpstreamPath(p string) string {
	p = normalizePath(p)
	if strings.HasPrefix(p, "/api/") {
		return p
	}
	if strings.HasPrefix(p, "/game3d/") || strings.HasPrefix(p, "/new-portal/") || strings.HasPrefix(p, "/3d/") {
		return "/api" + p
	}
	return p
}

// rewriteAndProxy sets the outbound path and runs the mode's reverse proxy.
func (s *Server) rewriteAndProxy(w http.ResponseWriter, r *http.Request, mode, outboundPath string, forceSign bool) {
	proxy := s.mainProxy
	if mode == ModeConcept {
		proxy = s.conceptProxy
		outboundPath = conceptUpstreamPath(outboundPath)
	}

	// Clone request with rewritten path; optionally buffer body for signing.
	var bodyBytes []byte
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		bodyBytes, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}

	// Main-site quotainfo requires sceneType; empty {} returns upstream HTTP 500.
	// Verified via Playwright against the live SPA.
	if mode == ModeMain && strings.Contains(outboundPath, "/api/3d/quotainfo") && r.Method == http.MethodPost {
		if len(bodyBytes) == 0 || string(bodyBytes) == "{}" || string(bodyBytes) == "null" {
			bodyBytes = []byte(`{"sceneType":"3dCreations"}`)
		}
	}

	q := r.URL.Query()
	needSign := forceSign || (r.Method == http.MethodPost && pathNeedsSignature(outboundPath))
	// Also sign main-mode POST creations when header asks
	if !needSign && r.Header.Get("X-Hy-Sign") == "1" && r.Method == http.MethodPost {
		needSign = true
	}

	if needSign && r.Method == http.MethodPost {
		extra := map[string]string{}
		if uid := jarValue(s.Jar, "m.Iq"); uid != "" {
			// not part of sign params necessarily; skip
			_ = uid
		}
		_, _, _, merged := SignedQuery(q, bodyBytes, extra)
		q = merged
	}

	outURL := &url.URL{
		Scheme:   s.upstreamURL.Scheme,
		Host:     s.upstreamURL.Host,
		Path:     outboundPath,
		RawQuery: q.Encode(),
	}

	outReq := r.Clone(r.Context())
	outReq.URL = outURL
	outReq.RequestURI = ""
	outReq.Host = s.upstreamURL.Host
	// SPA headers the upstream expects on authenticated XHRs.
	if outReq.Header.Get("Trace-Id") == "" && outReq.Header.Get("trace-id") == "" {
		outReq.Header.Set("Trace-Id", randomNonce(32))
	}
	if outReq.Header.Get("x-source") == "" {
		outReq.Header.Set("x-source", "web")
	}
	if outReq.Header.Get("x-product") == "" {
		outReq.Header.Set("x-product", "hunyuan3d")
	}
	if len(bodyBytes) > 0 {
		outReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		outReq.ContentLength = int64(len(bodyBytes))
		outReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
		if outReq.Header.Get("Content-Type") == "" {
			outReq.Header.Set("Content-Type", "application/json")
		}
	} else {
		outReq.Body = http.NoBody
		outReq.ContentLength = 0
	}

	// Stash signed debug info in a response header for local debugging.
	if needSign && r.Method == http.MethodPost {
		if sign := outURL.Query().Get(defaultSignField); sign != "" {
			w.Header().Set("X-Hy-Sign-Applied", "1")
			w.Header().Set("X-Hy-Timestamp", outURL.Query().Get(defaultTimestampField))
			w.Header().Set("X-Hy-Nonce", outURL.Query().Get(defaultNonceField))
		}
	}

	proxy.ServeHTTP(w, outReq)
}

// ProxyMain proxies to a main-site path (leading slash, no /api/main prefix).
func (s *Server) ProxyMain(w http.ResponseWriter, r *http.Request, path string) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	s.rewriteAndProxy(w, r, ModeMain, path, false)
}

// ProxyConcept proxies to a host path for concept mode.
func (s *Server) ProxyConcept(w http.ResponseWriter, r *http.Request, path string) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	s.rewriteAndProxy(w, r, ModeConcept, path, false)
}

// ProxyMode proxies using the resolved mode.
func (s *Server) ProxyMode(w http.ResponseWriter, r *http.Request, mode, path string) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	s.rewriteAndProxy(w, r, NormalizeMode(mode), path, false)
}

// catchAll extracts /api/main/{path} or /api/concept/{path} remainder.
func catchAll(path, prefix string) string {
	p := strings.TrimPrefix(path, prefix)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// clientIP is a best-effort remote address for logs.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		// naive IPv4:port
		if strings.Count(host, ":") == 1 {
			return host[:i]
		}
	}
	return host
}

// itoa is a tiny helper kept for possible future use.
func itoa(n int) string { return strconv.Itoa(n) }
