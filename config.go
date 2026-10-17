package main

import (
	"os"
	"strconv"
	"strings"
)

// UpstreamHost is the Hunyuan3D platform host (no scheme).
const UpstreamHost = "3d.hunyuan.tencent.com"

// DefaultUpstream is the default scheme+host base URL.
const DefaultUpstream = "https://3d.hunyuan.tencent.com"

// RefererMain is the browser Referer used for main-platform calls.
const RefererMain = "https://3d.hunyuan.tencent.com/"

// RefererConcept is the browser Referer used for concept-studio calls.
const RefererConcept = "https://3d.hunyuan.tencent.com/studio/creation/concept"

// DefaultUA is a desktop Chrome-like User-Agent.
const DefaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

// Modes
const (
	ModeMain    = "main"
	ModeConcept = "concept"
)

// Config holds runtime configuration from the environment.
type Config struct {
	Port     string
	DataDir  string
	Upstream string
}

// LoadConfig reads PORT / DATA_DIR / UPSTREAM from the environment.
func LoadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8787"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	up := os.Getenv("UPSTREAM")
	if up == "" {
		up = DefaultUpstream
	}
	up = strings.TrimRight(up, "/")
	return Config{
		Port:     port,
		DataDir:  dataDir,
		Upstream: up,
	}
}

// Host returns the bind host. Defaults to loopback: the console has no
// authentication of its own, and /mcp can spend credits without confirmation,
// so binding every interface must be an explicit choice (HOST=0.0.0.0).
func (c Config) Host() string {
	h := strings.TrimSpace(os.Getenv("HOST"))
	if h == "" {
		return "127.0.0.1"
	}
	return h
}

// Addr returns the listen address.
func (c Config) Addr() string {
	// An explicit ":port" keeps its meaning (all interfaces) for compatibility.
	if strings.HasPrefix(c.Port, ":") {
		return c.Port
	}
	// allow bare port
	if _, err := strconv.Atoi(c.Port); err == nil {
		return c.Host() + ":" + c.Port
	}
	return c.Host() + ":" + c.Port
}

// RefererFor returns the appropriate Referer for a mode.
func RefererFor(mode string) string {
	if mode == ModeConcept {
		return RefererConcept
	}
	return RefererMain
}

// NormalizeMode maps free-form input to main|concept.
func NormalizeMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case ModeConcept, "studio", "game3d", "concept-studio":
		return ModeConcept
	case ModeMain, "platform", "home", "":
		return ModeMain
	default:
		return ModeMain
	}
}

// IsValidMode reports whether s is a known mode name.
func IsValidMode(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == ModeMain || s == ModeConcept
}
