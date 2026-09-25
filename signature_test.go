package main

import (
	"encoding/hex"
	"crypto/hmac"
	"crypto/sha256"
	"testing"
)

func TestDeriveHMACKey(t *testing.T) {
	key := DeriveHMACKey()
	want := "Hf6d6KFB3D"
	if string(key) != want {
		t.Fatalf("key = %q (%x), want %q", key, key, want)
	}
	if len(key) != 10 {
		t.Fatalf("key len = %d, want 10", len(key))
	}
}

func TestSignParamsDeterministicFields(t *testing.T) {
	params := map[string]string{"prompt": "cat", "type": "text_to_3d"}
	signed, sign, ts, nonce := SignParams(params)
	if sign == "" || ts == "" || nonce == "" {
		t.Fatalf("missing sign/ts/nonce: %q %q %q", sign, ts, nonce)
	}
	if signed["sign"] != sign || signed["timestamp"] != ts || signed["nonce"] != nonce {
		t.Fatalf("signed map mismatch: %+v", signed)
	}
	if len(nonce) != 16 {
		t.Fatalf("nonce len = %d", len(nonce))
	}
	canonical := CanonicalQuery(map[string]string{
		"prompt":    "cat",
		"type":      "text_to_3d",
		"timestamp": ts,
		"nonce":     nonce,
	})
	mac := hmac.New(sha256.New, SignKey())
	mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if want != sign {
		t.Fatalf("sign mismatch got=%s want=%s canonical=%s", sign, want, canonical)
	}
	if len(sign) != 64 {
		t.Fatalf("sha256 hex sign length = %d, want 64", len(sign))
	}
}

func TestCanonicalQuerySkipsEmptyAndSorts(t *testing.T) {
	got := CanonicalQuery(map[string]string{
		"b": "2",
		"a": "1",
		"c": "",
	})
	want := "a=1&b=2"
	if got != want {
		t.Fatalf("canonical = %q, want %q", got, want)
	}
}

func TestPathNeedsSignature(t *testing.T) {
	if !pathNeedsSignature("/api/3d/creations/generations") {
		t.Fatal("generations should be signed")
	}
	if !pathNeedsSignature("/api/3d/creations/delete") {
		t.Fatal("delete should be signed")
	}
	if pathNeedsSignature("/api/3d/config") {
		t.Fatal("config GET need not be signed")
	}
	if pathNeedsSignature("/game3d/geometry_generation/text2geometry") {
		t.Fatal("concept geometry APIs are not signed on the web client")
	}
}

func TestCloudActionMap(t *testing.T) {
	if cloudVersion != "2025-05-13" {
		t.Fatalf("version = %q", cloudVersion)
	}
	if cloudHost != "ai3d.tencentcloudapi.com" {
		t.Fatalf("host = %q", cloudHost)
	}
	if cloudActions["submit-pro"] != "SubmitHunyuanTo3DProJob" {
		t.Fatalf("submit-pro map = %q", cloudActions["submit-pro"])
	}
	if cloudActions["query-pro"] != "QueryHunyuanTo3DProJob" {
		t.Fatalf("query-pro map = %q", cloudActions["query-pro"])
	}
	if len(cloudActions) < 10 {
		t.Fatalf("cloudActions too small: %d", len(cloudActions))
	}
	found := 0
	for _, v := range cloudActions {
		if v == "SubmitHunyuanTo3DProJob" || v == "QueryHunyuanTo3DProJob" {
			found++
		}
	}
	if found < 2 {
		t.Fatal("official actions not present in map values")
	}
}
