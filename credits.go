package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CreditResult is one labeled credit API call result.
type CreditResult struct {
	Source   string      `json:"source"`
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Mode     string      `json:"mode"`
	Status   int         `json:"status"`
	OK       bool        `json:"ok"`
	Body     interface{} `json:"body,omitempty"`
	Raw      string      `json:"raw,omitempty"`
	Full     string      `json:"-"` // untruncated body for list parsing (kept out of JSON logs)
	Error    string      `json:"error,omitempty"`
	Duration string      `json:"duration"`
}

// CreditBundle is an aggregated independent credit snapshot.
type CreditBundle struct {
	Mode    string                 `json:"mode"`
	Main    map[string]interface{} `json:"main,omitempty"`
	Concept map[string]interface{} `json:"concept,omitempty"`
	All     map[string]interface{} `json:"all,omitempty"`
	Note    string                 `json:"note"`
}

// callUpstream performs a credentialed upstream call through the proxy headers.
func (s *Server) callUpstream(r *http.Request, method, mode, path string, body []byte) CreditResult {
	start := time.Now()
	res := CreditResult{
		Method: method,
		Path:   path,
		Mode:   mode,
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := s.Cfg.Upstream + path
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	} else if method == http.MethodPost {
		rdr = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		res.Error = err.Error()
		res.Duration = time.Since(start).String()
		return res
	}
	req.Header.Set("User-Agent", DefaultUA)
	req.Header.Set("Origin", "https://"+UpstreamHost)
	req.Header.Set("Referer", RefererFor(mode))
	req.Header.Set("x-source", "web")
	req.Header.Set("x-product", "hunyuan3d")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	// SPA always sends Trace-Id (UUID without dashes).
	req.Header.Set("Trace-Id", strings.ReplaceAll(randomNonce(32), "-", ""))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if c := s.Jar.CookieHeader(); c != "" {
		req.Header.Set("Cookie", c)
	}
	if uid := jarValue(s.Jar, "hy_user"); uid != "" {
		req.Header.Set("x_hunyuan_inner_user_id", uid)
	} else if uid := jarValue(s.Jar, "m.Iq"); uid != "" {
		req.Header.Set("x_hunyuan_inner_user_id", uid)
	}

	// Sign POST generation/mutation family (main + concept).
	if method == http.MethodPost && pathNeedsSignature(path) {
		q := req.URL.Query()
		_, _, _, merged := SignedQuery(q, body, nil)
		req.URL.RawQuery = merged.Encode()
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	res.Duration = time.Since(start).String()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	s.Jar.AbsorbResponseCookies(resp.Header, req.URL)
	res.Status = resp.StatusCode
	// Keep full body for JSON parse; store a short preview in Raw for logs.
	full := string(raw)
	res.Full = full
	if len(full) > 2000 {
		res.Raw = full[:2000] + "...(truncated)"
	} else {
		res.Raw = full
	}
	var parsed interface{}
	if err := json.Unmarshal(raw, &parsed); err == nil {
		res.Body = parsed
		res.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	} else {
		res.Body = full
		res.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	}
	if !res.OK && res.Error == "" {
		res.Error = "upstream status " + resp.Status
	}
	return res
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// scrubResult redacts sensitive fields (phone/token/cookie…) from an upstream
// result before it reaches local API consumers (Web UI, logs). Preview strings
// that cannot be parsed as JSON are dropped entirely rather than leaked.
func scrubResult(res CreditResult) CreditResult {
	res.Full = ""
	switch b := res.Body.(type) {
	case string:
		var v interface{}
		if err := json.Unmarshal([]byte(b), &v); err == nil {
			res.Body = scrubSensitive(v)
		} else if b != "" {
			res.Body = ""
			res.Raw = ""
			return res
		}
	default:
		res.Body = scrubSensitive(res.Body)
	}
	if res.Raw != "" {
		var v interface{}
		if err := json.Unmarshal([]byte(res.Raw), &v); err == nil {
			if nb, err := json.Marshal(scrubSensitive(v)); err == nil {
				res.Raw = string(nb)
			}
		} else {
			res.Raw = ""
		}
	}
	return res
}

// MainCredits hits BOTH main credit APIs independently.
// Upstream verified via Playwright:
//   POST /api/3d/quotainfo  body MUST be {"sceneType":"3dCreations"} (empty {} → HTTP 500)
//   GET  /api/3d/planCreditInfo
func (s *Server) MainCredits(r *http.Request) map[string]interface{} {
	quota := scrubResult(s.callUpstream(r, http.MethodPost, ModeMain, "/api/3d/quotainfo",
		[]byte(`{"sceneType":"3dCreations"}`)))
	plan := scrubResult(s.callUpstream(r, http.MethodGet, ModeMain, "/api/3d/planCreditInfo", nil))
	// Friendly extracted numbers when quota body is the standard shape.
	var remain, total interface{}
	if body, ok := quota.Body.(map[string]interface{}); ok {
		remain = body["remainQuota"]
		total = body["totalQuota"]
	}
	return map[string]interface{}{
		"backend":        "main",
		"independent":    true,
		"remainQuota":    remain,
		"totalQuota":     total,
		"quotainfo":      quota,
		"planCreditInfo": plan,
		"note":           "main credits = POST /api/3d/quotainfo {\"sceneType\":\"3dCreations\"} + GET /api/3d/planCreditInfo; independent of concept remainingTimes",
	}
}

// ConceptCredits hits the concept credit API.
// Real upstream path is /api/game3d/general_info/get_remaining_times
// (bare /game3d/* is nginx 405; concept APIs are mounted under /api).
func (s *Server) ConceptCredits(r *http.Request) map[string]interface{} {
	body := []byte("{}")
	rem := scrubResult(s.callUpstream(r, http.MethodPost, ModeConcept, "/api/game3d/general_info/get_remaining_times", body))
	userinfo := scrubResult(s.callUpstream(r, http.MethodGet, ModeConcept, "/api/3d/getuserinfo", nil))
	return map[string]interface{}{
		"backend":           "concept",
		"independent":       true,
		"get_remaining_times": rem,
		"getuserinfo":       userinfo,
		"note":              "Concept credit API is independent of main quotainfo/planCreditInfo.",
	}
}

// AllCredits aggregates both independently (never merges balances).
func (s *Server) AllCredits(r *http.Request) map[string]interface{} {
	main := s.MainCredits(r)
	concept := s.ConceptCredits(r)
	return map[string]interface{}{
		"independent": true,
		"design":      "main and concept credit systems are separate upstream accounts/APIs; this proxy reports them side-by-side and does not sum or share balances.",
		"main":        main,
		"concept":     concept,
	}
}

// HandleCredit routes GET /api/credit/{main|concept|all}
func (s *Server) HandleCredit(w http.ResponseWriter, r *http.Request, which string) {
	switch strings.ToLower(which) {
	case "main":
		writeOKMode(w, ModeMain, s.MainCredits(r))
	case "concept":
		writeOKMode(w, ModeConcept, s.ConceptCredits(r))
	case "all":
		writeOK(w, s.AllCredits(r))
	default:
		writeErr(w, http.StatusNotFound, "unknown credit scope: "+which+" (use main|concept|all)")
	}
}

// proxyCreditPassthrough transparently proxies a single credit API and
// wraps the raw upstream body in the local envelope for convenience.
func (s *Server) proxyCreditPassthrough(w http.ResponseWriter, r *http.Request, mode, path string) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
	}
	method := r.Method
	if method == "" {
		method = http.MethodPost
	}
	res := s.callUpstream(r, method, mode, path, body)
	if res.Error != "" && res.Status == 0 {
		writeErrMode(w, http.StatusBadGateway, mode, res.Error)
		return
	}
	writeJSON(w, res.Status, APIEnvelope{OK: res.OK, Data: res, Mode: mode, Error: res.Error})
}

// sanitizeURL is kept for future upstream overrides.
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return DefaultUpstream
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return DefaultUpstream
	}
	return strings.TrimRight(raw, "/")
}
