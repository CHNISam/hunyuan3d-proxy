package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// mcpRemovedNotice explains the removal of the stdio transport, so an old
// client config fails with a fix rather than a confusing wrong-port server.
const mcpRemovedNotice = `error: the "-mcp" (stdio) mode has been removed.

MCP is now served over HTTP by the normal server:

  1) start the server:  ./hunyuan3d-proxy-linux
  2) MCP endpoint:      POST http://127.0.0.1:<PORT>/mcp   (Streamable HTTP)

Client config example:

  {"mcpServers":{"hunyuan3d":{"url":"http://127.0.0.1:8812/mcp"}}}

See docs/mcp.md for the full tool list and auth options.
`

// usageText is printed by -help.
const usageText = `hunyuan3d-proxy — 腾讯混元3D 反代服务器（仅学习用途）

用法:
  hunyuan3d-proxy                启动本地 HTTP 服务（默认 127.0.0.1:8787）
  hunyuan3d-proxy -help          显示本帮助
  hunyuan3d-proxy -version       显示版本

环境变量:
  PORT              HTTP 服务端口（默认 8787，README 脚本用 8812）
  HOST              绑定地址（默认 127.0.0.1；设 0.0.0.0 会暴露到局域网）
  DATA_DIR          Cookie 与会话目录（默认 ./data）
  UPSTREAM          上游地址（默认 https://3d.hunyuan.tencent.com）
  PROXY_QUIET=1     关闭 HTTP 访问日志
  MCP_HTTP_TOKEN    设置后 /mcp 需要 Authorization: Bearer <token>；不设置则不校验

MCP:
  MCP 服务随 HTTP 服务自动启用，端点 POST /mcp（Streamable HTTP），
  共 11 个工具；控制台 http://127.0.0.1:8812/ 的「MCP 服务」面板可查看工具与接入配置。
  详见 docs/mcp.md。
`

func main() {
	// Argument modes: help, version. Anything else (or no argument) runs the
	// HTTP proxy, which also serves the MCP endpoint at POST /mcp.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-mcp", "--mcp", "mcp":
			// The stdio transport was removed; MCP is now served at POST /mcp.
			// Fail loudly instead of silently starting a wrong server.
			fmt.Fprint(os.Stderr, mcpRemovedNotice)
			os.Exit(2)
		case "-help", "--help", "-h", "help":
			fmt.Print(usageText)
			return
		case "-version", "--version", "version":
			fmt.Printf("%s %s\n", mcpServerName, mcpServerVersion)
			return
		}
	}

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
	log.Printf("hunyuan3d-proxy listening on http://%s", addr)
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") && !strings.HasPrefix(addr, "[::1]:") {
		if mcpAuthEnabled() {
			log.Printf("WARN: bound to a non-loopback address %s; /mcp requires the MCP_HTTP_TOKEN bearer token", addr)
		} else {
			log.Printf("WARN: bound to a non-loopback address %s AND /mcp has no token (MCP_HTTP_TOKEN unset): "+
				"anyone who can reach this port can spend the account's credits", addr)
		}
	}
	log.Printf("upstream=%s mode=%s cookies=%s", cfg.Upstream, srv.DefaultMode(), cookiePath)
	log.Printf("health: http://%s/api/health", addr)

	handler := corsMiddleware(mux)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
