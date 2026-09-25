package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Hardcoded key-derivation material from web module 47436 (Xv).
var (
	sigC = []byte{122, 59, 92, 165, 30, 79, 166, 139, 142, 129, 139, 89, 219, 131, 101, 204}
	sigD = []byte{122, 59, 92, 45, 30, 79, 106, 139, 156, 13, 46, 63, 74, 91, 108, 125}
	sigU = []byte{3, 5, 2, 7, 1, 4, 6, 2, 5, 3, 1, 4, 2, 6, 3, 5}
	sigM = []byte{14, 11, 13, 9, 15, 10, 12, 8, 6, 3, 5, 1, 7, 2, 4, 0}
)

const (
	defaultNonceLength    = 16
	defaultTimestampField = "timestamp"
	defaultNonceField     = "nonce"
	defaultSignField      = "sign"
	nonceAlphabet         = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// DeriveHMACKey reproduces the JS key derivation:
// t[i]=c[i]^d[i]; o[i]=rotl8(t[i], u[i]); n[i]=o[m[i]];
// key = UTF-8 bytes of n up to first 0 (or full 16).
// Result: "Hf6d6KFB3D"
func DeriveHMACKey() []byte {
	var t, o, n [16]byte
	for i := 0; i < 16; i++ {
		t[i] = sigC[i] ^ sigD[i]
	}
	for i := 0; i < 16; i++ {
		rot := int(sigU[i]) & 7
		v := t[i]
		if rot == 0 {
			o[i] = v
		} else {
			o[i] = (v << rot) | (v >> (8 - rot))
		}
	}
	for i := 0; i < 16; i++ {
		n[i] = o[sigM[i]]
	}
	a := 16
	for i := 0; i < 16; i++ {
		if n[i] == 0 {
			a = i
			break
		}
	}
	key := make([]byte, a)
	copy(key, n[:a])
	return key
}

var derivedKey = DeriveHMACKey()

// SignKey returns the HMAC key used for request signing.
func SignKey() []byte { return derivedKey }

func randomNonce(n int) string {
	if n <= 0 {
		n = defaultNonceLength
	}
	b := make([]byte, n)
	alphabet := []byte(nonceAlphabet)
	if _, err := rand.Read(b); err != nil {
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = alphabet[int(now+int64(i))%len(alphabet)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// CanonicalQuery builds key=value&... with non-empty values, keys sorted.
func CanonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// SignCanonical computes Hex(HmacSHA256(canonical, key)).
// Web module 47436: CryptoJS.HmacSHA256(...).toString(CryptoJS.enc.Hex).
func SignCanonical(canonical string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// SignParams signs a flat string map. Returns signed copy + sign/ts/nonce.
func SignParams(params map[string]string) (signed map[string]string, sign, ts, nonce string) {
	g := make(map[string]string, len(params)+3)
	for k, v := range params {
		if v == "" {
			continue
		}
		g[k] = v
	}
	ts = strconv.FormatInt(time.Now().Unix(), 10)
	nonce = randomNonce(defaultNonceLength)
	g[defaultTimestampField] = ts
	g[defaultNonceField] = nonce
	canonical := CanonicalQuery(g)
	sign = SignCanonical(canonical, derivedKey)
	g[defaultSignField] = sign
	return g, sign, ts, nonce
}

func flattenJSONPrimitives(body []byte) map[string]string {
	out := map[string]string{}
	if len(body) == 0 {
		return out
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return out
	}
	for k, rv := range raw {
		out[k] = rawMessageToString(rv)
	}
	return out
}

func rawMessageToString(rv json.RawMessage) string {
	s := strings.TrimSpace(string(rv))
	if s == "" || s == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal(rv, &str); err == nil {
		return str
	}
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		return "" // skip nested structures
	}
	return s
}

// SignedQuery applies the Xv signature. Query params win over body scalars.
// sign/timestamp/nonce are attached as query parameters.
func SignedQuery(query url.Values, body []byte, extra map[string]string) (sign, timestamp, nonce string, merged url.Values) {
	params := map[string]string{}
	for k, v := range flattenJSONPrimitives(body) {
		params[k] = v
	}
	for k, v := range extra {
		if v != "" {
			params[k] = v
		}
	}
	for k, vs := range query {
		if len(vs) == 0 || vs[0] == "" {
			continue
		}
		params[k] = vs[0]
	}
	signed, sign, ts, nonce := SignParams(params)
	merged = url.Values{}
	for k, v := range signed {
		merged.Set(k, v)
	}
	for k, vs := range query {
		if merged.Get(k) == "" && len(vs) > 0 {
			merged[k] = vs
		}
	}
	merged.Set(defaultSignField, sign)
	merged.Set(defaultTimestampField, ts)
	merged.Set(defaultNonceField, nonce)
	return sign, ts, nonce, merged
}

// pathNeedsSignature reports whether a path opts into the axios
// signature interceptor (signature:!0) on the main site, or is part
// of the concept generation family we sign for forward-compat.
func pathNeedsSignature(path string) bool {
	raw := strings.ToLower(strings.TrimPrefix(path, "/"))
	// Work with both "api/3d/..." and "3d/..." forms.
	p := raw
	pNoAPI := strings.TrimPrefix(raw, "api/")

	signedExact := []string{
		"3d/creations/generations",
		"3d/creations/bonebindingfrommesh",
		"3d/creations/manualbonebindingfrommesh",
		"3d/creations/manualbonebinding",
		"3d/creations/cancel",
		"3d/creations/count",
		"3d/creations/retopologize",
		"3d/creations/delete",
		"3d/creations/resourceconvert",
		"3d/resource/genuploadinfo",
		"3d/resource/review",
		"3d/resource/cmykcheck",
		"3d/worldplay/text2image",
		"3d/worldplay/page2video",
		"3d/worldscene/review",
		"3d/feedback/submit",
		"3d/purchasebasicplan",
		"3d/notice/list",
		"3d/notice/statusupdate",
	}
	for _, s := range signedExact {
		if pNoAPI == s || p == s || p == "api/"+s {
			return true
		}
	}
	// creations/* family on main
	if strings.HasPrefix(pNoAPI, "3d/creations/") {
		return true
	}
	// Concept /game3d/* uses a different client (no signature interceptor).
	return false
}

// SignatureDebug returns a self-test payload for docs/health.
func SignatureDebug() map[string]interface{} {
	key := DeriveHMACKey()
	params := map[string]string{
		"prompt": "a cute robot",
		"type":   "text_to_3d",
	}
	signed, sign, ts, nonce := SignParams(params)
	return map[string]interface{}{
		"key_hex":     fmt.Sprintf("%x", key),
		"key_string":  string(key),
		"key_len":     len(key),
		"sample_sign": sign,
		"timestamp":   ts,
		"nonce":       nonce,
		"algorithm":   "Hex(HmacSHA256(canonical, key))",
		"docs":        "https://cloud.tencent.com/document/product/1804/120829",
		"canonical":   CanonicalQuery(map[string]string{"prompt": "a cute robot", "type": "text_to_3d", "timestamp": ts, "nonce": nonce}),
		"signed_keys": len(signed),
	}
}
