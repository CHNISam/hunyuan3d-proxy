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
