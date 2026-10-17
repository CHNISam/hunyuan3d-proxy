//go:build researchlive

package main

// Explicit, interactive research probe. Excluded from ordinary offline tests.
// Login remains in-process: no public login API and no persistent cookies.
import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResearchWeChatReadOnlySession(t *testing.T) {
	if os.Getenv("RESEARCH_INTERACTIVE_WECHAT") != "1" {
		t.Skip("requires explicit interactive login opt-in")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal("workspace unavailable")
	}
	statusPath := filepath.Join(root, "evidence", "wechat-login-status.json")
	report := map[string]interface{}{"provider": "wechat", "state": "starting", "generation_calls": 0,
		"texture_calls": 0, "cloud_calls": 0, "credentials": "process memory only"}
	save := func(state string) {
		report["state"] = state
		raw, _ := json.MarshalIndent(report, "", "  ")
		if os.WriteFile(statusPath, raw, 0o600) != nil {
			t.Fatal("cannot save sanitized status")
		}
		fmt.Println("RESEARCH_WECHAT_STATE=" + state)
	}
	dataDir := t.TempDir()
	srv, err := NewServer(Config{Port: "0", DataDir: dataDir, Upstream: DefaultUpstream})
	if err != nil {
		t.Fatal("proxy initialization failed")
	}
	srv.Jar = NewJar("")
	defer func() {
		srv.Jar.Clear()
		_ = os.Remove(filepath.Join(root, "evidence", "wechat-login-qr.jpg"))
		_ = os.Remove(filepath.Join(root, "evidence", "wechat-qr-url.txt"))
		report["temporary_qr_files_removed"] = true
		files, _ := os.ReadDir(dataDir)
		report["session_files_written"] = len(files) != 0
		report["session_cleared"] = true
		save(fmt.Sprint(report["state"]))
	}()
	login := NewLoginService(srv)
	save("requesting_qr")
	qr, err := wxStart()
	if err != nil {
		report["failure_stage"] = "official WeChat QR retrieval"
		// No OAuth identifiers or response bodies in logs.
		report["error_type"] = fmt.Sprintf("%T", err)
		save("qr_failed")
		t.Fatal("official WeChat QR retrieval failed; sanitized status saved")
	}
	qrPath := filepath.Join(root, "evidence", "wechat-login-qr.jpg")
	if os.WriteFile(qrPath, qr.QR, 0o600) != nil {
		t.Fatal("cannot save QR image")
	}
	// Display the original Tencent HTTPS image without giving a browser access
	// to local files. The temporary QR locator is not an account session.
	if os.WriteFile(filepath.Join(root, "evidence", "wechat-qr-url.txt"),
		[]byte("https://open.weixin.qq.com/connect/qrcode/"+qr.uuid), 0o600) != nil {
		t.Fatal("cannot save temporary official image locator")
	}
	report["qr_image"] = "evidence/wechat-login-qr.jpg"
	save("waiting_for_user_scan")
	deadline := time.Now().Add(5 * time.Minute)
	last := ""
	for time.Now().Before(deadline) {
		wxPoll(login, qr.ID)
		if qr.status != last {
			save(qr.status)
			last = qr.status
		}
		if qr.status == "success" {
			break
		}
		if qr.status == "failed" || qr.status == "expired" {
			report["failure_stage"] = "official scan or session exchange"
			save(qr.status)
			t.Fatal("login did not complete; no raw response logged")
		}
		time.Sleep(time.Second)
	}
	if !srv.Jar.HasSessionHeuristic() {
		save("scan_timeout")
		t.Fatal("login scan timed out")
	}
	report["session_obtained"] = true
	save("logged_in_querying_read_only")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("loopback listener unavailable")
	}
	address := listener.Addr().String()
	token := randomNonce(64)
	t.Setenv("MCP_HTTP_TOKEN", token)
	t.Setenv("RESEARCH_ALLOW_SPEND", "0")
	t.Setenv("RESEARCH_ALLOW_SESSION_IMPORT", "0")
	httpServer := &http.Server{Handler: researchGate(address, buildMux(srv, login)), ReadHeaderTimeout: 5 * time.Second}
	go httpServer.Serve(listener)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python", filepath.Join(root, "upstream", "research", "session_readonly_query.py"))
	cmd.Env = append(os.Environ(), "RESEARCH_QUERY_URL=http://"+address, "RESEARCH_QUERY_TOKEN="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if cmd.Run() != nil {
		report["failure_stage"] = "authenticated read-only MCP/query check"
		save("login_obtained_query_failed")
		t.Fatal("read-only verification did not pass")
	}
	report["read_only_query_passed"] = true
	save("verified_and_stopping")
}
