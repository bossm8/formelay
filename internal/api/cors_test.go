package api

import (
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed(t *testing.T) {
	allowed := []string{"https://example.com", "https://*.wild.example"}
	cases := []struct {
		name, origin string
		want         bool
	}{
		{"exact match", "https://example.com", true},
		{"wildcard subdomain match", "https://foo.wild.example", true},
		{"wildcard bare apex also matches", "https://wild.example", true},
		{"scheme mismatch rejected", "http://foo.wild.example", false},
		{"suffix-confusion domain rejected", "https://foo.wild.example.evil.com", false},
		{"unrelated origin rejected", "https://evil.example", false},
		{"empty origin rejected", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := originAllowed(allowed, c.origin); got != c.want {
				t.Fatalf("originAllowed(%v, %q) = %v, want %v", allowed, c.origin, got, c.want)
			}
		})
	}
}

func TestWriteCORSHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCORSHeaders(rec, "https://example.com", "X-Formelay-Site-Key")

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the matched origin echoed back, never a bare '*'", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "X-Formelay-Site-Key, Content-Type" {
		t.Fatalf("Access-Control-Allow-Headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("Access-Control-Max-Age = %q", got)
	}
}
