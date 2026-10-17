package main

import "testing"

func TestScrubSensitive(t *testing.T) {
	in := map[string]interface{}{
		"phoneNumber":                "15100000898",
		"token":                      "abcdef123456",
		"encryptCompletePhoneNumber": "LHG+HkBnoppIImWf8yPaFw+IBCEsmvJ1UBFMKLqnSzw=",
		"hyTokenPresent":             true,
		"cookieNames":                []interface{}{"hy_token", "hy_user"},
		"cookieCount":                7,
		"empty":                      map[string]interface{}{"token": ""},
		"nested":                     map[string]interface{}{"access_token": "zzz123456789"},
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

// sortAssetsByTime must interleave the two upstream timestamp formats.
// Regression: concept returns ISO-8601 while main returns Unix seconds; when
// only numbers parsed, every concept task scored 0 and sank behind every main
// task, so a freshly generated concept model never reached the first page.
func TestSortAssetsByTimeMixedFormats(t *testing.T) {
	iso := func(s string) interface{} { return s }
	assets := []AssetItem{
		{ID: "concept-old", Source: "concept", CreatedAt: iso("2026-09-27T02:15:07Z")},
		{ID: "main-mid", Source: "main", CreatedAt: float64(1790488386)},
		{ID: "concept-new", Source: "concept", CreatedAt: iso("2026-10-16T16:23:40.736Z")},
		{ID: "main-new", Source: "main", CreatedAt: float64(1790489280)},
		{ID: "missing", Source: "main", CreatedAt: nil},
	}
	sortAssetsByTime(assets)

	order := make([]string, len(assets))
	for i, a := range assets {
		order[i] = a.ID
	}
	// newest first; the nil timestamp sorts last
	want := []string{"concept-new", "main-new", "main-mid", "concept-old", "missing"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestParseAssetTimeFormats(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"2026-10-16T16:23:40.736Z", 1792167820},
		{"2026-09-27T02:15:07Z", 1790475307},
		{"2026-09-27 02:15:07", 1790475307},
		{"2026-09-27", 1790467200},
	} {
		got, err := parseAssetTime(tc.in)
		if err != nil {
			t.Errorf("parseAssetTime(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseAssetTime(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseAssetTime("not-a-time"); err == nil {
		t.Error("unrecognised time must error, not silently return 0")
	}
	if _, err := parseAssetTime(""); err == nil {
		t.Error("empty time must error")
	}
}
