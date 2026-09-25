package main

// Pure-Go WeChat QR login (open.weixin qrconnect protocol) — no browser:
//  1. GET qrconnect page            → embedded uuid
//  2. GET /connect/qrcode/<uuid>    → scanable JPEG
//  3. long-poll /connect/l/qrconnect → wx_errcode / wx_code
//       408 waiting | 201 scanned | 405/200 confirmed (wx_code = OAuth jsCode)
//  4. POST /api/login/joint {type,jsCode,appid} → session cookies into jar

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const wxAppID = "wx01cc05677df34280"

type wxQRSession struct {
	ID         string
	QR         []byte
	uuid       string
	redirect   string
	status     string // pending | scanned | expired | success | failed
	message    string
	code       string
	expireHit  int
	created    time.Time
}

var (
	wxMu    sync.Mutex
	wxStore = map[string]*wxQRSession{}
)

func wxLookup(id string) *wxQRSession {
	wxMu.Lock()
	defer wxMu.Unlock()
	return wxStore[id]
}

var (
	wxUUIDRe = regexp.MustCompile(`uuid["'\s:=]+([A-Za-z0-9_-]{6,})`)
	wxErrRe  = regexp.MustCompile(`wx_errcode\s*=\s*(\d+)`)
	wxCodeRe = regexp.MustCompile(`wx_code\s*=\s*['"]([^'"]*)['"]`)
)

func wxStart() (*wxQRSession, error) {
	n := randomNonce(16)
	redirect := "https://" + UpstreamHost + "/scan?nonce=" + url.QueryEscape(n)
	authorize := "https://open.weixin.qq.com/connect/qrconnect?appid=" + wxAppID +
		"&scope=snsapi_login&redirect_uri=" + url.QueryEscape(redirect) +
		"&state=wechat_login&login_type=jssdk&self_redirect=false"

	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, authorize, nil)
	req.Header.Set("User-Agent", DefaultUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	html, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	_ = resp.Body.Close()

	var uuid string
	if m := wxUUIDRe.FindSubmatch(html); m != nil {
		uuid = string(m[1])
	}
	if uuid == "" {
		return nil, fmt.Errorf("qrconnect 页面未找到 uuid（HTTP %d）", resp.StatusCode)
	}

	qrReq, _ := http.NewRequest(http.MethodGet, "https://open.weixin.qq.com/connect/qrcode/"+uuid, nil)
	qrReq.Header.Set("User-Agent", DefaultUA)
	qrReq.Header.Set("Referer", "https://open.weixin.qq.com/")
	qrResp, err := client.Do(qrReq)
	if err != nil {
		return nil, err
	}
	defer qrResp.Body.Close()
	png, _ := io.ReadAll(io.LimitReader(qrResp.Body, 4<<20))
	if len(png) < 200 {
		return nil, fmt.Errorf("二维码图片获取失败（HTTP %d，%d 字节）", qrResp.StatusCode, len(png))
	}

	s := &wxQRSession{
		ID:       randomNonce(20),
		QR:       png,
		uuid:     uuid,
		redirect: redirect,
		status:   "pending",
		message:  "请用微信扫码，确认后自动写入会话",
		created:  time.Now(),
	}
	wxMu.Lock()
	wxStore[s.ID] = s
	wxMu.Unlock()
	return s, nil
}

// wxPoll long-polls the qrconnect endpoint; on confirm it exchanges wx_code
// via the official /api/login/joint API.
func wxPoll(ls *LoginService, id string) map[string]interface{} {
	s := wxLookup(id)
	if s == nil {
		return map[string]interface{}{"ticket": id, "status": "failed", "message": "会话不存在或已过期"}
	}
	if s.status != "success" && s.status != "failed" && s.status != "expired" {
		poll := "https://lp.open.weixin.qq.com/connect/l/qrconnect?uuid=" + url.QueryEscape(s.uuid) +
			"&tip=1&r=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		req, _ := http.NewRequest(http.MethodGet, poll, nil)
		req.Header.Set("User-Agent", DefaultUA)
		req.Header.Set("Referer", "https://open.weixin.qq.com/")
		client := &http.Client{Timeout: 25 * time.Second}
		if resp, err := client.Do(req); err != nil {
			s.message = err.Error()
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			_ = resp.Body.Close()
			text := string(body)
			code := ""
			if m := wxErrRe.FindStringSubmatch(text); m != nil {
				code = m[1]
			}
			switch code {
			case "405", "200":
				wcode := ""
				if m := wxCodeRe.FindStringSubmatch(text); m != nil {
					wcode = m[1]
				}
				if wcode == "" {
					s.status = "failed"
					s.message = "确认成功但未返回 wx_code"
					break
				}
				s.code = wcode
				s.status = "scanned"
				s.message = "扫码成功，正在换会话…"
				if err := wxFinish(ls, s); err != nil {
					s.status = "failed"
					s.message = err.Error()
				}
			case "201":
				s.status = "scanned"
				s.message = "已扫码，请在手机上确认"
			case "404":
				// transient 404 happens mid-scan; only expire after repeated hits
				s.expireHit++
				if s.expireHit >= 3 {
					s.status = "expired"
					s.message = "二维码已失效，请重新生成"
				} else {
					s.message = "等待扫码"
				}
			case "408", "":
				s.status = "pending"
				s.message = "等待扫码"
			default:
				// 500 / others: transient — keep polling
				s.message = "等待扫码"
			}
		}
	}
	return wxPublic(s, ls)
}

func wxFinish(ls *LoginService, s *wxQRSession) error {
	result := ls.jointLogin("wechat", s.code, wxAppID)
	if ls.srv.Jar.HasSessionHeuristic() {
		s.status = "success"
		s.message = "登录成功，会话已写入"
		return nil
	}
	snippet, _ := result["body"].(string)
	if len(snippet) > 120 {
		snippet = snippet[:120]
	}
	return fmt.Errorf("code 已拿到但 /api/login/joint 未下发会话（%s），请用 Cookie 导入", snippet)
}

func wxPublic(s *wxQRSession, ls *LoginService) map[string]interface{} {
	out := map[string]interface{}{
		"ticket":    s.ID,
		"provider":  "wechat",
		"status":    s.status,
		"message":   s.message,
		"qrProxy":   "/api/login/qr/image?session=" + url.QueryEscape(s.ID),
		"code":      s.code != "",
		"createdAt": s.created.Unix(),
	}
	if ls != nil {
		out["login"] = ls.currentState()
	}
	return out
}
