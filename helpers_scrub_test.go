package main

import "testing"

func TestScrubSensitive(t *testing.T) {
	in := map[string]interface{}{
		"phoneNumber":               "15100000898",
		"token":                     "abcdef123456",
		"encryptCompletePhoneNumber": "LHG+HkBnoppIImWf8yPaFw+IBCEsmvJ1UBFMKLqnSzw=",
		"hyTokenPresent":            true,
		"cookieNames":               []interface{}{"hy_token", "hy_user"},
		"cookieCount":               7,
		"empty":                     map[string]interface{}{"token": ""},
		"nested":                    map[string]interface{}{"access_token": "zzz123456789"},
	}
	out := scrubSensitive(in).(map[string]interface{})

	if out["hyTokenPresent"] != true {
		t.Fatal("presence flag hyTokenPresent must stay boolean true")
	}
	if out["cookieCount"] != 7 {
		t.Fatal("cookieCount must stay numeric")
	}
	if names, ok := out["cookieNames"].([]interface{}); !ok || names[0] != "hy_token" {
		t.Fatal("cookieNames (names only) must stay readable")
	}
	if out["phoneNumber"] == "15100000898" {
		t.Fatal("phoneNumber not masked")
	}
	if out["token"] == "abcdef123456" {
		t.Fatal("token not masked")
	}
	if out["encryptCompletePhoneNumber"] == "LHG+HkBnoppIImWf8yPaFw+IBCEsmvJ1UBFMKLqnSzw=" {
		t.Fatal("encryptCompletePhoneNumber not masked")
	}
	if out["empty"].(map[string]interface{})["token"] != "" {
		t.Fatal("empty values should stay empty (absence, not a secret)")
	}
	if out["nested"].(map[string]interface{})["access_token"] == "zzz123456789" {
		t.Fatal("nested access_token not masked")
	}
}

func TestMaskID(t *testing.T) {
	if maskID("") != "" {
		t.Fatal("empty id stays empty")
	}
	if maskID("short") != "***" {
		t.Fatal("short ids fully redacted")
	}
	m := maskID("smoke_test_uid")
	if m == "smoke_test_uid" || m == "" {
		t.Fatalf("id not masked: %q", m)
	}
}

func TestScrubResultDropsUnparseablePreview(t *testing.T) {
	res := CreditResult{Raw: `{"errNo":0}...not json`, Body: `{"errNo":0} trailing junk`, Full: "secret-full"}
	res = scrubResult(res)
	if res.Full != "" {
		t.Fatal("Full must be cleared")
	}
	if res.Raw != "" {
		t.Fatalf("unparseable raw must be dropped, got %q", res.Raw)
	}
	if res.Body != "" {
		t.Fatalf("unparseable body must be dropped, got %q", res.Body)
	}
}
