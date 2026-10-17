package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CookieEntry is a persisted cookie with optional expiry metadata.
type CookieEntry struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
	SameSite string    `json:"sameSite,omitempty"`
}

// Jar is a simple in-memory cookie jar backed by a JSON file.
// Main and concept traffic share this account session (upstream is one
// logged-in Tencent account); credit APIs remain independent upstream.
type Jar struct {
	mu   sync.RWMutex
	path string
	list []CookieEntry
}

// NewJar loads (or creates) a cookie jar at the given file path.
func NewJar(path string) *Jar {
	j := &Jar{path: path}
	j.load()
	return j
}

func (j *Jar) load() {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return
	}
	var list []CookieEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	j.mu.Lock()
	j.list = list
	j.mu.Unlock()
}

// Save writes the jar to disk (best-effort create parent dirs).
func (j *Jar) Save() error {
	j.mu.RLock()
	path := j.path
	if path == "" {
		j.mu.RUnlock()
		return nil // TASK-206: explicitly memory-only research session.
	}
	data, err := json.MarshalIndent(j.list, "", "  ")
	j.mu.RUnlock()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o600)
}

// Snapshot returns a copy of all cookies.
func (j *Jar) Snapshot() []CookieEntry {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]CookieEntry, len(j.list))
	copy(out, j.list)
	return out
}

// Len returns cookie count.
func (j *Jar) Len() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.list)
}

// Set inserts or replaces a cookie by name (domain soft-match).
func (j *Jar) Set(c CookieEntry) {
	if c.Path == "" {
		c.Path = "/"
	}
	if c.Domain == "" {
		c.Domain = UpstreamHost
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range j.list {
		if j.list[i].Name == c.Name {
			j.list[i] = c
			return
		}
	}
	j.list = append(j.list, c)
}

// SetMap sets cookies from a name->value map. Returns count written.
func (j *Jar) SetMap(m map[string]string) int {
	n := 0
	for k, v := range m {
		if k == "" {
			continue
		}
		j.Set(CookieEntry{
			Name:     k,
			Value:    v,
			Domain:   "." + UpstreamHost,
			Path:     "/",
			HTTPOnly: true,
		})
		n++
	}
	return n
}

// Delete removes a cookie by name.
func (j *Jar) Delete(name string) {
	j.mu.Lock()
	out := make([]CookieEntry, 0, len(j.list))
	for _, c := range j.list {
		if c.Name != name {
			out = append(out, c)
		}
	}
	j.list = out
	j.mu.Unlock()
}

// Clear removes all cookies.
func (j *Jar) Clear() {
	j.mu.Lock()
	j.list = nil
	j.mu.Unlock()
}

// CookieHeader builds a Cookie request header value for the upstream.
func (j *Jar) CookieHeader() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if len(j.list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(j.list))
	for _, c := range j.list {
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// AbsorbResponseCookies merges Set-Cookie headers from an upstream response
// into the jar.
func (j *Jar) AbsorbResponseCookies(hdr http.Header, upstream *url.URL) int {
	cookies := hdr.Values("Set-Cookie")
	if len(cookies) == 0 {
		return 0
	}
	fake := &http.Response{Header: hdr}
	parsed := fake.Cookies()
	n := 0
	host := UpstreamHost
	if upstream != nil && upstream.Host != "" {
		host = upstream.Hostname()
	}
	for _, c := range parsed {
		entry := CookieEntry{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HttpOnly,
		}
		if entry.Domain == "" {
			entry.Domain = host
		}
		if entry.Path == "" {
			entry.Path = "/"
		}
		if c.MaxAge > 0 {
			entry.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
		} else if !c.Expires.IsZero() {
			entry.Expires = c.Expires
		}
		switch c.SameSite {
		case http.SameSiteLaxMode:
			entry.SameSite = "Lax"
		case http.SameSiteStrictMode:
			entry.SameSite = "Strict"
		case http.SameSiteNoneMode:
			entry.SameSite = "None"
		}
		j.Set(entry)
		n++
	}
	return n
}

// ImportHTTPCookies converts []*http.Cookie into the jar.
func (j *Jar) ImportHTTPCookies(cookies []*http.Cookie) int {
	n := 0
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		entry := CookieEntry{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HttpOnly,
		}
		if entry.Domain == "" {
			entry.Domain = UpstreamHost
		}
		if entry.Path == "" {
			entry.Path = "/"
		}
		j.Set(entry)
		n++
	}
	return n
}

// CookieNames returns just the names currently stored.
func (j *Jar) CookieNames() []string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	names := make([]string, 0, len(j.list))
	for _, c := range j.list {
		names = append(names, c.Name)
	}
	return names
}

// authCookieKeys are known Hunyuan3D / Tencent session keys.
var authCookieKeys = []string{
	"hy_user", "hy_token", "userInfo", "hy_global_config", "hy_device_id",
	"m.Iq", "x_hunyuan_inner_user_id",
	"hy3d_token", "hy3d_uid", "hy3d_session",
	"uin", "skey", "p_skey", "p_uin", "o_uin",
	"openid", "openid_hy3d", "access_token", "session", "sessionid",
	"login_sid", "pskey_guid", "uid", "user_id", "hy_user_id",
}

// HasSessionHeuristic reports whether the jar looks logged-in.
func (j *Jar) HasSessionHeuristic() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	have := map[string]string{}
	for _, c := range j.list {
		have[c.Name] = c.Value
	}
	for _, k := range authCookieKeys {
		if v, ok := have[k]; ok && v != "" {
			return true
		}
	}
	for _, c := range j.list {
		if len(c.Value) > 8 {
			lower := strings.ToLower(c.Name)
			for _, frag := range []string{"token", "session", "login", "skey", "openid", "uin", "hy_user"} {
				if strings.Contains(lower, frag) {
					return true
				}
			}
		}
	}
	return len(j.list) >= 5
}

// JarPath returns the on-disk path.
func (j *Jar) JarPath() string { return j.path }

// SetPath updates the on-disk path (used at startup).
func (j *Jar) SetPath(p string) {
	j.mu.Lock()
	j.path = p
	j.mu.Unlock()
}

// Reload re-reads the jar file from disk.
func (j *Jar) Reload() {
	j.load()
}

// DebugString is a short human summary (no secret values).
func (j *Jar) DebugString() string {
	return fmt.Sprintf("jar path=%s count=%d names=%v session_like=%v",
		j.JarPath(), j.Len(), j.CookieNames(), j.HasSessionHeuristic())
}
