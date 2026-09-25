package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Worker-backed WeChat/QQ login sessions (Playwright).
// Worker: tools/hy_login_worker.js  default http://127.0.0.1:8793

func loginWorkerBase() string {
	if v := os.Getenv("LOGIN_WORKER"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8793"
}

type workerSession struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Status       string   `json:"status"`
	Message      string   `json:"message"`
	Nonce        string   `json:"nonce"`
	AuthorizeURL string   `json:"authorizeUrl"`
	RedirectURI  string   `json:"redirectUri"`
	QRImageURL   string   `json:"qrImageUrl"`
	QRProxyURL   string   `json:"qrProxyUrl"`
	HasCookies   bool     `json:"hasCookies"`
	CookieNames  []string `json:"cookieNames"`
}

func workerPost(path string, body interface{}) (map[string]interface{}, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, loginWorkerBase()+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("worker %s: %s", path, string(raw))
	}
	return out, nil
}

func workerGet(path string) (map[string]interface{}, int, error) {
	req, err := http.NewRequest(http.MethodGet, loginWorkerBase()+path, nil)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out, resp.StatusCode, nil
}

// Legal WeChat authorize URL matching official login page embed.
// redirect_uri MUST be https://3d.hunyuan.tencent.com/scan?nonce=...
// (verified via Playwright — /login or localhost callback is rejected)
func legalWeChatAuthorize(nonceStr string) (authorize, redirect string) {
	if nonceStr == "" {
		nonceStr = randomNonce(12)
	}
	redirect = "https://" + UpstreamHost + "/scan?nonce=" + url.QueryEscape(nonceStr)
	q := url.Values{}
	q.Set("appid", "wx01cc05677df34280")
	q.Set("scope", "snsapi_login")
	q.Set("redirect_uri", redirect)
	q.Set("state", "wechat_login")
	q.Set("login_type", "jssdk")
	q.Set("self_redirect", "false")
	authorize = "https://open.weixin.qq.com/connect/qrconnect?" + q.Encode()
	return authorize, redirect
}

func legalQQAuthorize(nonceStr string) (authorize, redirect string) {
	if nonceStr == "" {
		nonceStr = randomNonce(12)
	}
	// QQ whitelist appears to be site login page; local callback returns 100010.
	redirect = "https://" + UpstreamHost + "/login"
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "102788748")
	q.Set("redirect_uri", redirect)
	q.Set("state", "qq_login")
	q.Set("nonce", nonceStr)
	authorize = "https://graph.qq.com/oauth2.0/authorize?" + q.Encode()
	return authorize, redirect
}

// ---- rewrite QR start to use legal redirect + optional Playwright worker ----

type loginSessionLocal struct {
	mu       sync.Mutex
	workerID string
	provider string
	ticket   string
}

// HandleQRStart — updated flow:
// 1) Always emit LEGAL authorize URLs (WeChat → /scan?nonce, QQ → /login).
// 2) Try Playwright worker to open official page and fetch REAL QR image.
// 3) Return qrImageUrl + local proxy path for the UI.
func (ls *LoginService) HandleQRStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "wechat"
	}
	provider = strings.ToLower(provider)
	mode := ls.srv.ResolveMode(r, "")
	ticket := ls.newTicket()

	var authorize, redirect string
	n := ticket
	if len(n) > 16 {
		n = n[:16]
	}
	if provider == "qq" {
		authorize, redirect = legalQQAuthorize(n)
	} else {
		provider = "wechat"
		authorize, redirect = legalWeChatAuthorize(n)
	}

	payload := map[string]string{
		"provider":     provider,
		"authorizeUrl": authorize,
		"redirectUri":  redirect,
		"state":        ticket,
		"note":         "redirect_uri must stay on 3d.hunyuan.tencent.com (WeChat/QQ whitelist)",
	}

	// Try Playwright worker for a real session + official QR bitmap.
	workerTried := false
	var workerData map[string]interface{}
	out, err := workerPost("/session/start", map[string]string{"provider": provider})
	workerTried = true
	if err == nil {
		if d, ok := out["data"].(map[string]interface{}); ok {
			workerData = d
			if s, ok := d["status"].(string); ok {
				payload["workerStatus"] = s
			}
			if id, ok := d["id"].(string); ok {
				payload["workerSession"] = id
				ticket = id // UI polls worker session id
			}
			if au, ok := d["authorizeUrl"].(string); ok && au != "" {
				authorize = au
				payload["authorizeUrl"] = au
			}
			if rd, ok := d["redirectUri"].(string); ok && rd != "" {
				redirect = rd
				payload["redirectUri"] = rd
			}
			if qr, ok := d["qrProxyUrl"].(string); ok && qr != "" {
				payload["qrProxyUrl"] = qr
			}
			if qru, ok := d["qrImageUrl"].(string); ok && qru != "" {
				payload["qrImageUrl"] = qru
			}
		}
	} else {
		payload["workerError"] = err.Error()
	}

	sess := &QRSession{
		Ticket: ticket, Provider: provider, URL: authorize, Callback: redirect,
		Payload: payload, Mode: mode, CreatedAt: time.Now(), Status: "pending",
		Message: "use official QR (worker image) or open authorizeUrl; after scan import cookies if worker cannot capture",
	}
	ls.mu.Lock()
	ls.qr[ticket] = sess
	ls.mu.Unlock()

	writeOKMode(w, mode, map[string]interface{}{
		"ticket":        ticket,
		"provider":      provider,
		"url":           authorize,
		"redirectUri":   redirect,
		"payload":       payload,
		"workerTried":   workerTried,
		"worker":        workerData,
		"qrImageUrl":    payload["qrImageUrl"],
		"qrProxyUrl":    payload["qrProxyUrl"],
		"mode":          mode,
		"status":        "pending",
		"login":         ls.currentState(),
		"cookieHint":    "after WeChat confirm, cookies land on *.hunyuan.tencent.com/scan — import via POST /api/login/cookies or worker export",
		"manualCookie":  "browser DevTools → copy hy_user / hy_token → POST /api/login/cookies",
	})
}

// HandleQRStatus — also poll Playwright worker when ticket is a worker session id.
func (ls *LoginService) HandleQRStatus(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		writeErr(w, http.StatusBadRequest, "ticket required")
		return
	}

	// Worker session?
	if out, code, err := workerGet("/session/" + url.PathEscape(ticket)); err == nil && code == 200 {
		if d, ok := out["data"].(map[string]interface{}); ok {
			status, _ := d["status"].(string)
			// If worker captured cookies, export into our jar automatically.
			if status == "success" || status == "redirected" || d["hasCookies"] == true {
				if exp, err2 := workerPost("/session/"+url.PathEscape(ticket)+"/export", map[string]string{}); err2 == nil {
					if exp["ok"] == true {
						if arr, ok := exp["cookies"].([]interface{}); ok {
							cookies := map[string]string{}
							for _, it := range arr {
								m, _ := it.(map[string]interface{})
								if m == nil {
									continue
								}
								name, _ := m["name"].(string)
								val, _ := m["value"].(string)
								if name != "" && val != "" {
									cookies[name] = val
								}
							}
							ls.storeCookies(cookies)
							d["exportedToJar"] = true
							d["jarState"] = ls.currentState()
						}
					}
				}
			}
			writeOK(w, map[string]interface{}{
				"ticket":  ticket,
				"source":  "playwright-worker",
				"status":  status,
				"worker":  d,
				"login":   ls.currentState(),
				"qrProxy": "/api/login/qr/image?session=" + url.QueryEscape(ticket),
			})
			return
		}
	}

	// Local in-memory session fallback
	ls.mu.Lock()
	sess, ok := ls.qr[ticket]
	ls.mu.Unlock()
	state := ls.currentState()
	status := "unknown"
	msg := ""
	urlStr := ""
	if ok {
		status = sess.Status
		msg = sess.Message
		urlStr = sess.URL
		if state.SessionLike {
			status = "success"
		}
	} else if state.SessionLike {
		status = "success"
	}
	writeOK(w, map[string]interface{}{
		"ticket": ticket,
		"status": status,
		"message": msg,
		"url":    urlStr,
		"login":  state,
	})
}

// HandleQRImage proxies the official WeChat QR PNG from the worker session.
func (ls *LoginService) HandleQRImage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	imageURL := r.URL.Query().Get("url")
	if sessionID == "" && imageURL == "" {
		writeErr(w, http.StatusBadRequest, "session or url query required")
		return
	}
	src := imageURL
	if src == "" {
		src = loginWorkerBase() + "/session/" + url.PathEscape(sessionID) + "/qr"
	}
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, 502, "qr fetch failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
