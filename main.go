package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func readJSONBody(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return io.EOF
	}
	return json.Unmarshal(body, v)
}

// corsMiddleware enables local-dev CORS and basic request logging.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || origin == "null" {
			// file:// pages and some privacy modes send Origin: null
			origin = "*"
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Credentials", "false")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie, X-Hy-Mode, X-Hy-Sign, X-Requested-With, x-source, x-product")
		w.Header().Set("Access-Control-Expose-Headers", "X-Hy-Sign-Applied, X-Hy-Timestamp, X-Hy-Nonce")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		if os.Getenv("PROXY_QUIET") != "1" {
			log.Printf("%s %s %s %s", clientIP(r), r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

func main() {
	cfg := LoadConfig()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	cookiePath := filepath.Join(cfg.DataDir, "cookies.json")

	srv, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}
	srv.Jar.SetPath(cookiePath)
	srv.Jar.Reload()

	login := NewLoginService(srv)
	mux := buildMux(srv, login)

	addr := cfg.Addr()
	log.Printf("hunyuan3d-proxy listening on http://127.0.0.1%s", addr)
	log.Printf("upstream=%s mode=%s cookies=%s", cfg.Upstream, srv.DefaultMode(), cookiePath)
	log.Printf("health: http://127.0.0.1%s/api/health", addr)

	handler := corsMiddleware(mux)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
