package main

// QR login sessions — pure Go, no browser automation:
//   WeChat → qrconnect protocol (uuid + lp long-poll) → /api/login/joint  (wxlogin.go)
//   QQ     → deprecated (QQ互联回调校验 + ptqrlogin 403) → Cookie 导入
// Email / phone / cookie-import login are plain upstream API calls.

import (
	"net/http"
	"net/url"
	"strings"
)

// Legal WeChat authorize URL matching the official login page embed.
// redirect_uri MUST be https://3d.hunyuan.tencent.com/scan?nonce=...
// (verified against the live SPA — /login or localhost callback is rejected)
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

// HandleQRStart issues a scanable QR via the pure-Go WeChat protocol.
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

	// QQ 扫码已废弃：QQ互联收紧回调校验 + ptqrlogin 对非浏览器客户端 403。
	if provider == "qq" {
		writeOKMode(w, mode, map[string]interface{}{
			"provider":   "qq",
			"status":     "unsupported",
			"message":    "QQ 扫码登录已废弃：请使用 Cookie 导入（登录官网后复制 hy_user / hy_token 粘贴到控制台「Cookie 导入」）",
			"cookieHint": "浏览器打开 3d.hunyuan.tencent.com 登录 → F12 → Application → Cookies → 复制 hy_user / hy_token → 控制台「Cookie 导入」",
			"useCookie":  true,
			"login":      ls.currentState(),
		})
		return
	}

	sess, err := wxStart()
	if err != nil {
		writeErr(w, 502, "微信二维码获取失败: "+err.Error())
		return
	}
	authorize, _ := legalWeChatAuthorize("")
	writeOKMode(w, mode, map[string]interface{}{
		"ticket":      sess.ID,
		"provider":    "wechat",
		"url":         authorize,
		"redirectUri": sess.redirect,
		"qrProxyUrl":  "/api/login/qr/image?session=" + url.QueryEscape(sess.ID),
		"status":      "pending",
		"message":     "请用微信扫码，确认后自动写入会话",
		"login":       ls.currentState(),
		"cookieHint":  "扫码确认后自动调 /api/login/joint 换会话入库；失败时用 Cookie 导入",
	})
}

// HandleQRStatus polls the pure-Go WeChat session.
func (ls *LoginService) HandleQRStatus(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		writeErr(w, http.StatusBadRequest, "ticket required")
		return
	}

	if wxLookup(ticket) != nil {
		writeOK(w, wxPoll(ls, ticket))
		return
	}

	// Local session fallback (tickets created by the OAuth callback).
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
		"ticket":  ticket,
		"status":  status,
		"message": msg,
		"url":     urlStr,
		"login":   state,
	})
}

// HandleQRImage serves the stored QR bitmap for a WeChat session.
func (ls *LoginService) HandleQRImage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "session query required")
		return
	}
	s := wxLookup(sessionID)
	if s == nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	ctype := http.DetectContentType(s.QR)
	if ctype == "application/octet-stream" {
		ctype = "image/png"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.QR)
}
