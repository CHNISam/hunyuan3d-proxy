package main

// Local TASK-206 containment, not an upstream release or commercial license.
import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

// researchGate wraps the public listener. MCP's in-process dispatch remains
// inside this boundary; its catalogue has no arbitrary HTTP dispatch tool.
func researchGate(address string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != address || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+address) {
			writeErr(w, http.StatusForbidden, "research origin/host refused")
			return
		}
		if !mcpAuthorized(r) || mcpToken() == "" {
			writeErr(w, http.StatusUnauthorized, "research bearer token required")
			return
		}
		spend := os.Getenv("RESEARCH_ALLOW_SPEND") == "1"
		switch r.URL.Path {
		case "/mcp":
			if r.Method == http.MethodPost {
				body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
				if err != nil || len(body) > 1<<20 {
					writeErr(w, http.StatusBadRequest, "invalid research request body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var rpc struct {
					Method string `json:"method"`
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if json.Unmarshal(body, &rpc) == nil && rpc.Method == "tools/call" {
					switch rpc.Params.Name {
					case "hunyuan_health", "hunyuan_set_mode", "hunyuan_assets", "hunyuan_works", "hunyuan_main_creations":
					case "hunyuan_generate_concept", "hunyuan_generate_main", "hunyuan_texture":
						if !spend {
							writeErr(w, http.StatusForbidden, "research spending is locked")
							return
						}
					default:
						writeErr(w, http.StatusForbidden, "tool outside research allowlist")
						return
					}
				}
			}
		case "/api/health", "/api/login/status", "/api/credit/main", "/api/credit/concept", "/api/credit/all", "/api/text3d/works", "/api/text3d/assets":
			if r.Method != http.MethodGet {
				writeErr(w, http.StatusMethodNotAllowed, "GET required")
				return
			}
		case "/api/text3d/main/list":
			if r.Method != http.MethodPost {
				writeErr(w, http.StatusMethodNotAllowed, "POST required")
				return
			}
		case "/api/login/cookies":
			if os.Getenv("RESEARCH_ALLOW_SESSION_IMPORT") != "1" {
				writeErr(w, http.StatusNotFound, "session import is disabled")
				return
			}
			if r.Method != http.MethodPost {
				writeErr(w, http.StatusMethodNotAllowed, "POST required")
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 16385))
			var session struct {
				Cookies map[string]string `json:"cookies"`
				Raw     string            `json:"raw"`
			}
			if err != nil || len(body) > 16384 || json.Unmarshal(body, &session) != nil ||
				session.Raw != "" || len(session.Cookies) != 2 ||
				session.Cookies["hy_user"] == "" || session.Cookies["hy_token"] == "" {
				writeErr(w, http.StatusBadRequest, "only hy_user and hy_token are accepted")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		case "/api/text3d/main", "/api/text3d/generate", "/api/text3d/texture":
			if !spend {
				writeErr(w, http.StatusForbidden, "research spending is locked")
				return
			}
		default:
			// No static file fallback, QR login, panel token,
			// raw upstream passthrough, cloud credentials, billing or purchases.
			writeErr(w, http.StatusNotFound, "outside research allowlist")
			return
		}
		next.ServeHTTP(w, r)
	})
}
