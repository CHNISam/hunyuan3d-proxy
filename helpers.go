package main

import (
	"net/http"
	"strings"
)

// Server wires config, cookie jar, default mode, and reverse proxies.
// Implementation lives in proxy.go; this file holds shared helpers used
// by login/credits that used to sit next to jar helpers.

// innerUserID returns hy_user / m.Iq / x_hunyuan_inner_user_id from the jar.
func innerUserID(jar *Jar) string {
	if v := jarValue(jar, "hy_user"); v != "" {
		return v
	}
	if v := jarValue(jar, "m.Iq"); v != "" {
		return v
	}
	return jarValue(jar, "x_hunyuan_inner_user_id")
}

// withInnerUser sets x_hunyuan_inner_user_id when known.
func withInnerUser(req *http.Request, jar *Jar) {
	if uid := innerUserID(jar); uid != "" {
		req.Header.Set("x_hunyuan_inner_user_id", uid)
	}
}

// normalizePath ensures a leading slash.
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// ---- sensitive-field redaction for Web UI facing responses ----

// maskID partially redacts an identifier: keep a short prefix/suffix so the
// value stays recognizable without exposing the whole ID.
func maskID(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:6] + "…" + s[len(s)-2:]
}

// maskValue redacts a sensitive string (phone, token, encrypted payload…).
func maskValue(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:3] + "…" + s[len(s)-2:]
}

// sensitiveKeys are matched after lowercasing and stripping "_" / "-".
// Only exact matches, so presence flags like hyTokenPresent / cookieNames
// stay intact.
var sensitiveKeys = map[string]bool{
	"token": true, "accesstoken": true, "refreshtoken": true, "idtoken": true,
	"hytoken": true, "hy3dtoken": true, "authtoken": true,
	"authorization": true, "cookie": true, "setcookie": true,
	"password": true, "passwd": true, "secret": true, "secretid": true,
	"secretkey": true, "apikey": true,
	"phone": true, "phonenumber": true, "mobile": true,
	"encryptcompletephonenumber": true, "encryptphonenumber": true,
	"encryptemail": true, "email": true,
	"openid": true, "skey": true, "pskey": true, "uin": true, "puin": true,
	"userinfo": true, "sessionkey": true, "userid": true, "hyuserid": true,
}

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, "-", "")
	return k
}

// scrubSensitive walks JSON-like data and masks values whose key names a
// credential or PII field. Maps are redacted in place.
func scrubSensitive(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if sensitiveKeys[normalizeKey(k)] {
				if s, ok := val.(string); ok {
					if s != "" {
						t[k] = maskValue(s)
					}
				} else if val != nil {
					t[k] = "***"
				}
				continue
			}
			t[k] = scrubSensitive(val)
		}
		return t
	case []interface{}:
		for i, val := range t {
			t[i] = scrubSensitive(val)
		}
		return t
	default:
		return v
	}
}
