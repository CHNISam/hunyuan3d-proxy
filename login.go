package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// QRSession tracks a local multi-provider login attempt.
type QRSession struct {
	Ticket    string            `json:"ticket"`
	Provider  string            `json:"provider"` // wechat | qq | email | phone | cookie
	URL       string            `json:"url"`
	Callback  string            `json:"callback"`
	Payload   map[string]string `json:"payload,omitempty"`
	AppID     string            `json:"appId,omitempty"`
	Mode      string            `json:"mode"`
	CreatedAt time.Time         `json:"createdAt"`
	Status    string            `json:"status"` // pending | scanned | success | expired | failed | unknown
	Message   string            `json:"message,omitempty"`
	Code      string            `json:"code,omitempty"`
	AuthResp  string            `json:"authResp,omitempty"`
}

type LoginState struct {
	LoggedIn    bool     `json:"loggedIn"`
	CookieCount int      `json:"cookieCount"`
	CookieNames []string `json:"cookieNames"`
	SessionLike bool     `json:"sessionLike"`
	JarPath     string   `json:"jarPath"`
	InnerUserID string   `json:"innerUserId,omitempty"`
	HyToken     bool     `json:"hyTokenPresent"`
	HyUser      bool     `json:"hyUserPresent"`
	Notes       string   `json:"notes"`
}

type LoginService struct {
	srv *Server
	mu  sync.Mutex
	qr  map[string]*QRSession
}

func NewLoginService(srv *Server) *LoginService {
	return &LoginService{srv: srv, qr: map[string]*QRSession{}}
}

func (ls *LoginService) newTicket() string { return randomNonce(24) }

func (ls *LoginService) publicBase(r *http.Request) string {
	// Prefer request host so OAuth redirect hits THIS proxy.
	host := r.Host
	if host == "" {
		host = "127.0.0.1:" + ls.srv.Cfg.Port
	}
	if !strings.Contains(host, ":") {
		host = host + ":" + ls.srv.Cfg.Port
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + host
}

// ---- cookie import / status / logout (same as before, compact) ----

type importCookiesReq struct {
	Cookies map[string]string `json:"cookies"`
	Raw     string            `json:"raw,omitempty"`
}

func (ls *LoginService) HandleImportCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req importCookiesReq
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if req.Cookies == nil {
		req.Cookies = map[string]string{}
	}
	if hdr := r.Header.Get("Cookie"); hdr != "" {
		for k, v := range parseCookieHeader(hdr) {
			req.Cookies[k] = v
		}
	}
	if req.Raw != "" {
		for k, v := range parseCookieHeader(req.Raw) {
			req.Cookies[k] = v
		}
	}
	if len(req.Cookies) == 0 {
		writeErr(w, http.StatusBadRequest, `send {"cookies":{"hy_token":"...","hy_user":"..."}}`)
		return
	}
	n := ls.storeCookies(req.Cookies)
	writeOK(w, map[string]interface{}{
		"imported":    n,
		"cookieNames": ls.srv.Jar.CookieNames(),
		"sessionLike": ls.srv.Jar.HasSessionHeuristic(),
		"state":       ls.currentState(),
	})
}

func (ls *LoginService) storeCookies(cookies map[string]string) int {
	authKeys := map[string]bool{
		"hy_user": true, "hy_token": true, "userInfo": true,
		"hy_global_config": true, "hy_device_id": true,
		"m.Iq": true, "x_hunyuan_inner_user_id": true,
		"uin": true, "skey": true, "p_skey": true, "p_uin": true,
		"o_uin": true, "openid": true, "hy3d_token": true, "hy3d_uid": true,
	}
	n := 0
	for k, v := range cookies {
		if k == "" || v == "" {
			continue
		}
		domain := UpstreamHost
		if authKeys[k] {
			domain = "." + UpstreamHost
		}
		ls.srv.Jar.Set(CookieEntry{Name: k, Value: v, Domain: domain, Path: "/", HTTPOnly: true, Secure: true})
		n++
	}
	if hyUser := cookies["hy_user"]; hyUser != "" {
		ls.srv.Jar.Set(CookieEntry{Name: "x_hunyuan_inner_user_id", Value: hyUser, Domain: "." + UpstreamHost, Path: "/"})
		ls.srv.Jar.Set(CookieEntry{Name: "m.Iq", Value: hyUser, Domain: "." + UpstreamHost, Path: "/"})
	}
	_ = ls.srv.Jar.Save()
	return n
}

func parseCookieHeader(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		out[strings.TrimSpace(part[:eq])] = strings.TrimSpace(part[eq+1:])
	}
	return out
}

// captureSetCookies extracts cookies from an HTTP response into the jar.
func (ls *LoginService) captureSetCookies(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	n := 0
	for _, c := range resp.Cookies() {
		domain := c.Domain
		if domain == "" {
			domain = UpstreamHost
		}
		ls.srv.Jar.Set(CookieEntry{
			Name: c.Name, Value: c.Value, Domain: domain, Path: c.Path,
			Secure: c.Secure, HTTPOnly: c.HttpOnly,
		})
		n++
	}
	if n > 0 {
		_ = ls.srv.Jar.Save()
	}
	return n
}

func (ls *LoginService) currentState() LoginState {
	jar := ls.srv.Jar
	st := LoginState{
		LoggedIn:    jar.HasSessionHeuristic(),
		CookieCount: jar.Len(),
		CookieNames: jar.CookieNames(),
		SessionLike: jar.HasSessionHeuristic(),
		JarPath:     jar.JarPath(),
		InnerUserID: jarValue(jar, "hy_user"),
		HyToken:     jarValue(jar, "hy_token") != "",
		HyUser:      jarValue(jar, "hy_user") != "",
	}
	if st.InnerUserID == "" {
		st.InnerUserID = jarValue(jar, "m.Iq")
	}
	if st.LoggedIn {
		st.Notes = "session ready; proxy forwards cookies + x_hunyuan_inner_user_id"
	} else {
		st.Notes = "scan QR (wechat/qq), complete email login, or POST /api/login/cookies"
	}
	return st
}

func (ls *LoginService) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeOK(w, ls.currentState())
}

func (ls *LoginService) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	ls.srv.Jar.Clear()
	_ = ls.srv.Jar.Save()
	writeOK(w, map[string]interface{}{"loggedOut": true, "state": ls.currentState()})
}

// ---- providers / start ----

func (ls *LoginService) fetchJointAppIDs() map[string]string {
	out := map[string]string{}
	base := ls.srv.Cfg.Upstream
	req, err := http.NewRequest(http.MethodGet, base+"/api/jointappid", nil)
	if err != nil {
		return out
	}
	req.Header.Set("User-Agent", DefaultUA)
	req.Header.Set("Origin", "https://"+UpstreamHost)
	req.Header.Set("Referer", RefererMain)
	req.Header.Set("x-source", "web")
	req.Header.Set("x-product", "hunyuan3d")
	if c := ls.srv.Jar.CookieHeader(); c != "" {
		req.Header.Set("Cookie", c)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) != nil {
		return out
	}
	for k, v := range parsed {
		if s, ok := v.(string); ok && s != "" {
			out[strings.ToLower(k)] = s
		}
	}
	return out
}

// HandleProviders GET /api/login/providers
func (ls *LoginService) HandleProviders(w http.ResponseWriter, r *http.Request) {
	apps := ls.fetchJointAppIDs()
	writeOK(w, map[string]interface{}{
		"providers": []map[string]string{
			{"id": "wechat", "label": "WeChat QR", "oauth": "open.weixin.qq.com/connect/qrconnect", "start": "/api/login/qr/start?provider=wechat"},
			{"id": "qq", "label": "QQ OAuth", "oauth": "graph.qq.com/oauth2.0/authorize", "start": "/api/login/qr/start?provider=qq"},
			{"id": "email", "label": "Email code", "start": "/api/login/email/send-code + /api/login/email/verify"},
			{"id": "phone", "label": "SMS code", "start": "/api/login/phone/send-code + /api/login/phone/verify"},
			{"id": "cookie", "label": "Cookie import", "start": "POST /api/login/cookies"},
		},
		"jointAppIds": apps,
		"redirectPolicy": map[string]string{
			"wechat": "https://3d.hunyuan.tencent.com/scan?nonce=...",
			"qq":     "https://3d.hunyuan.tencent.com/login",
		},
	})
}

// HandleOAuthCallback GET /api/login/oauth/callback?code=&state=&ticket=&provider=
// WeChat/QQ OAuth redirects here. We exchange code upstream and persist cookies.
func (ls *LoginService) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	ticket := q.Get("ticket")
	provider := q.Get("provider")
	if provider == "" {
		provider = "wechat"
	}
	if ticket == "" {
		ticket = state
	}

	ls.mu.Lock()
	sess, ok := ls.qr[ticket]
	if !ok {
		sess = &QRSession{Ticket: ticket, Provider: provider, CreatedAt: time.Now(), Status: "pending"}
		ls.qr[ticket] = sess
	}
	if code != "" {
		sess.Code = code
		if sess.Status == "pending" {
			sess.Status = "scanned"
			sess.Message = "OAuth code received, exchanging session..."
		}
	}
	ls.mu.Unlock()

	result := map[string]interface{}{
		"ticket":   ticket,
		"provider": sess.Provider,
		"code":     code != "",
		"state":    state,
	}

	if code != "" {
		apps := ls.fetchJointAppIDs()
		appID := sess.AppID
		if appID == "" {
			if sess.Provider == "qq" {
				appID = apps["qq"]
			} else {
				appID = apps["wx"]
			}
		}
		// 1) Try main-site open/auth (signature may be required by upstream)
		exchange := ls.exchangeOAuth(r, provider, appID, code, ticket)
		result["exchange"] = exchange

		// 2) Probe whether session is now valid
		st := ls.currentState()
		result["login"] = st
		ls.mu.Lock()
		if st.SessionLike {
			sess.Status = "success"
			sess.Message = "login success; cookies saved to jar"
		} else {
			sess.Status = "scanned"
			sess.Message = "code received but upstream did not set session cookies; import browser cookies or retry"
		}
		result["status"] = sess.Status
		result["message"] = sess.Message
		ls.mu.Unlock()
	}

	// Browser-facing redirect back to Web UI so status refreshes automatically.
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") || q.Get("format") == "html" {
		dest := ls.publicBase(r) + "/?login=1&provider=" + url.QueryEscape(provider) +
			"&ticket=" + url.QueryEscape(ticket) +
			"&status=" + url.QueryEscape(fmt.Sprint(result["status"]))
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}
	writeOK(w, result)
}

// exchangeOAuth calls candidate upstream auth endpoints and captures Set-Cookie.
func (ls *LoginService) exchangeOAuth(r *http.Request, provider, appID, code, state string) map[string]interface{} {
	base := ls.srv.Cfg.Upstream
	// signature: use empty / code / our web signature as fallbacks 鈥?upstream may ignore.
	sigs := []string{"", code, "1"}
	paths := []string{
		"/api/open/auth",
		"/api/auth-center/oauth2/callback",
		"/api/auth-center/login/callback",
		"/api/login/oauth/callback",
		"/3d/login/callback",
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// capture cookies on redirects too via jar-less manual handle
			return nil
		},
	}
	attempts := []map[string]interface{}{}
	for _, path := range paths {
		for _, sig := range sigs {
			q := url.Values{}
			q.Set("appID", appID)
			q.Set("appId", appID)
			q.Set("code", code)
			q.Set("state", state)
			q.Set("provider", provider)
			if sig != "" {
				q.Set("signature", sig)
			}
			u := base + path + "?" + q.Encode()
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				continue
			}
			req.Header.Set("User-Agent", DefaultUA)
			req.Header.Set("Accept", "application/json,text/html,*/*")
			req.Header.Set("Origin", "https://"+UpstreamHost)
			req.Header.Set("Referer", RefererMain)
			req.Header.Set("x-source", "web")
			req.Header.Set("x-product", "hunyuan3d")
			if c := ls.srv.Jar.CookieHeader(); c != "" {
				req.Header.Set("Cookie", c)
			}
			resp, err := client.Do(req)
			if err != nil {
				attempts = append(attempts, map[string]interface{}{"url": path, "error": err.Error()})
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			captured := ls.captureSetCookies(resp)
			// also parse JSON for token-like fields
			var parsed map[string]interface{}
			_ = json.Unmarshal(body, &parsed)
			snippet := string(body)
			if len(snippet) > 240 {
				snippet = snippet[:240] + "..."
			}
			attempts = append(attempts, map[string]interface{}{
				"path": path, "status": resp.StatusCode, "setCookie": captured,
				"body": snippet,
			})
			_ = resp.Body.Close()
			if ls.srv.Jar.HasSessionHeuristic() && resp.StatusCode < 400 {
				return map[string]interface{}{"ok": true, "used": path, "attempts": attempts, "state": ls.currentState()}
			}
		}
	}
	// JSON body token salvage
	return map[string]interface{}{
		"ok": false,
		"note": "upstream exchange inconclusive; scan in browser on 3d.hunyuan.tencent.com then import cookies if needed",
		"attempts": attempts,
		"state":    ls.currentState(),
	}
}

// HandleQRStatus is implemented in login_qr.go (Playwright worker aware).

// ---- email / phone login (prop-injected upstream paths are unknown; probe candidates) ----

var emailCodePaths = []string{
	"/api/auth-center/email/sendCode",
	"/api/auth-center/email/send-code",
	"/api/auth-center/email/code",
	"/api/login/email/sendCode",
	"/api/login/email/send-code",
	"/api/3d/login/email/sendCode",
	"/api/auth-center/verification/email",
	"/api/auth-center/captcha/email",
}

var emailVerifyPaths = []string{
	"/api/auth-center/email/login",
	"/api/auth-center/email/verify",
	"/api/auth-center/email/confirm",
	"/api/login/email/login",
	"/api/login/email/verify",
	"/api/3d/login/email",
	"/api/auth-center/login/email",
}

var phoneCodePaths = []string{
	"/api/auth-center/phone/sendCode",
	"/api/auth-center/phone/send-code",
	"/api/auth-center/sms/send",
	"/api/login/phone/sendCode",
	"/api/login/sms/sendCode",
	"/api/auth-center/captcha/sms",
}

var phoneVerifyPaths = []string{
	"/api/auth-center/phone/login",
	"/api/auth-center/phone/verify",
	"/api/auth-center/sms/login",
	"/api/login/phone/login",
	"/api/login/sms/login",
	"/api/auth-center/login/phone",
}

func (ls *LoginService) probeLoginPaths(w http.ResponseWriter, r *http.Request, kind string, paths []string, body []byte) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	base := ls.srv.Cfg.Upstream
	client := &http.Client{Timeout: 12 * time.Second}
	attempts := []map[string]interface{}{}
	for _, p := range paths {
		req, err := http.NewRequest(http.MethodPost, base+p, strings.NewReader(string(body)))
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", DefaultUA)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Origin", "https://"+UpstreamHost)
		req.Header.Set("Referer", RefererMain)
		req.Header.Set("x-source", "web")
		req.Header.Set("x-product", "hunyuan3d")
		if c := ls.srv.Jar.CookieHeader(); c != "" {
			req.Header.Set("Cookie", c)
		}
		resp, err := client.Do(req)
		if err != nil {
			attempts = append(attempts, map[string]interface{}{"path": p, "error": err.Error()})
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		captured := ls.captureSetCookies(resp)
		snippet := string(b)
		if len(snippet) > 220 {
			snippet = snippet[:220] + "..."
		}
		attempts = append(attempts, map[string]interface{}{
			"path": p, "status": resp.StatusCode, "setCookie": captured, "body": snippet,
		})
		_ = resp.Body.Close()
		// stop early on obvious success
		if resp.StatusCode == 200 && !strings.Contains(strings.ToLower(snippet), "error") &&
			!strings.Contains(snippet, "404") && !strings.Contains(snippet, "Not Found") {
			if kind == "verify" && ls.srv.Jar.HasSessionHeuristic() {
				writeOK(w, map[string]interface{}{"ok": true, "used": p, "attempts": attempts, "state": ls.currentState()})
				return
			}
			if kind == "code" {
				writeOK(w, map[string]interface{}{"ok": true, "used": p, "attempts": attempts, "hint": "if you received a code, call verify"})
				return
			}
		}
	}
	writeOK(w, map[string]interface{}{
		"ok":      false,
		"kind":    kind,
		"attempts": attempts,
		"state":   ls.currentState(),
		"note":    "upstream did not confirm; host login page injects exact paths 鈥?use cookie import or WeChat/QQ OAuth if probes fail",
	})
}

func (ls *LoginService) HandleEmailSendCode(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload map[string]interface{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if _, ok := payload["email"]; !ok {
		writeErr(w, http.StatusBadRequest, "body requires email")
		return
	}
	if _, ok := payload["captchaTicket"]; !ok {
		payload["captchaTicket"] = ""
		payload["captchaRandstr"] = ""
	}
	nb, _ := json.Marshal(payload)
	ls.probeLoginPaths(w, r, "code", emailCodePaths, nb)
}

func (ls *LoginService) HandleEmailVerify(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload map[string]interface{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if _, ok := payload["email"]; !ok {
		writeErr(w, http.StatusBadRequest, "body requires email")
		return
	}
	if _, ok := payload["verificationCode"]; !ok {
		writeErr(w, http.StatusBadRequest, "body requires verificationCode")
		return
	}
	nb, _ := json.Marshal(payload)
	ls.probeLoginPaths(w, r, "verify", emailVerifyPaths, nb)
}

func (ls *LoginService) HandlePhoneSendCode(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload map[string]interface{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if _, ok := payload["phone"]; !ok {
		if v, ok2 := payload["phoneN"]; ok2 {
			payload["phone"] = v
		} else if v, ok2 := payload["phoneNumber"]; ok2 {
			payload["phone"] = v
		} else {
			writeErr(w, http.StatusBadRequest, "body requires phone")
			return
		}
	}
	if _, ok := payload["captchaTicket"]; !ok {
		payload["captchaTicket"] = ""
		payload["captchaRandstr"] = ""
	}
	nb, _ := json.Marshal(payload)
	ls.probeLoginPaths(w, r, "code", phoneCodePaths, nb)
}

func (ls *LoginService) HandlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload map[string]interface{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if _, ok := payload["verificationCode"]; !ok {
		writeErr(w, http.StatusBadRequest, "body requires verificationCode")
		return
	}
	nb, _ := json.Marshal(payload)
	ls.probeLoginPaths(w, r, "verify", phoneVerifyPaths, nb)
}
