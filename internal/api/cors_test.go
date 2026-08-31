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

func TestOriginAllowedDangerousDisable(t *testing.T) {
	allowed := []string{"https://example.com", DangerousDisableOriginCheck}
	cases := []struct {
		name, origin string
	}{
		{"matches an arbitrary unrelated origin", "https://totally-unrelated.example"},
		{"matches an empty origin too, unlike every other entry type", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !originAllowed(allowed, c.origin) {
				t.Fatalf("originAllowed(%v, %q) = false, want true: %s disables origin checking entirely", allowed, c.origin, DangerousDisableOriginCheck)
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

// TestWriteCORSHeadersEmptyOrigin covers the one path that can reach
// writeCORSHeaders with an empty origin: DangerousDisableOriginCheck also
// admits a request with no Origin header at all, and there's no real
// value to echo back in that case.
func TestWriteCORSHeadersEmptyOrigin(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCORSHeaders(rec, "", "X-Formelay-Site-Key")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want a literal '*' when there's no real origin to echo", got)
	}
}
