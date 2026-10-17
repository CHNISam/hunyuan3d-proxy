package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchMemoryOnlyJar(t *testing.T) {
	dir := t.TempDir()
	jar := NewJar(filepath.Join(dir, "cookies.json"))
	jar.SetPath("")
	jar.Set(CookieEntry{Name: "hy_token", Value: "synthetic-test-session"})
	if err := jar.Save(); err != nil {
		t.Fatalf("memory-only session save must not write a file: %v", err)
	}
	if jar.Len() != 1 || !strings.Contains(jar.CookieHeader(), "synthetic-test-session") {
		t.Fatal("session must remain usable in memory")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("memory-only session left files on disk")
	}
}

func TestResearchSessionImportOptIn(t *testing.T) {
	t.Setenv("MCP_HTTP_TOKEN", "synthetic-local-bearer")
	t.Setenv("RESEARCH_ALLOW_SESSION_IMPORT", "0")
	srv, err := NewServer(Config{Port: "18812", DataDir: t.TempDir(), Upstream: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	srv.Jar.SetPath("")
	handler := researchGate("127.0.0.1:18812", buildMux(srv, NewLoginService(srv)))
	request := func(payload, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18812/api/login/cookies", strings.NewReader(payload))
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	payload := `{"cookies":{"hy_user":"synthetic-user","hy_token":"synthetic-session"}}`
	if got := request(payload, "synthetic-local-bearer").Code; got != 404 {
		t.Fatalf("import must default closed, got %d", got)
	}
	t.Setenv("RESEARCH_ALLOW_SESSION_IMPORT", "1")
	if got := request(payload, "").Code; got != 401 {
		t.Fatalf("import must require bearer, got %d", got)
	}
	if got := request(payload, "synthetic-local-bearer").Code; got != 200 {
		t.Fatalf("explicit authenticated import failed, got %d", got)
	}
	if got := request(`{"raw":"uin=synthetic"}`, "synthetic-local-bearer").Code; got != 400 {
		t.Fatalf("broad cookie imports must be refused, got %d", got)
	}
	if got := request(`{"cookies":{"hy_user":"synthetic-user","hy_token":"synthetic-session","p_skey":"qq-session"}}`, "synthetic-local-bearer").Code; got != 400 {
		t.Fatalf("QQ account cookies must be refused, got %d", got)
	}
}
