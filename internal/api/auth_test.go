package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bossm8/formelay/internal/config"
)

func TestExtractSiteKey(t *testing.T) {
	t.Run("header transport (default)", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-Formelay-Site-Key", "abc")
		got := extractSiteKey(r, nil, config.AuthConfig{})
		if got != "abc" {
			t.Fatalf("extractSiteKey = %q, want %q", got, "abc")
		}
	})

	t.Run("header transport with custom header name", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-Custom-Key", "abc")
		got := extractSiteKey(r, nil, config.AuthConfig{HeaderName: "X-Custom-Key"})
		if got != "abc" {
			t.Fatalf("extractSiteKey = %q, want %q", got, "abc")
		}
	})

	t.Run("form_field transport reads from decoded fields, not headers", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(defaultSiteKeyHeader, "should-be-ignored")
		fields := map[string]string{"site_key": "abc"}
		got := extractSiteKey(r, fields, config.AuthConfig{Transport: "form_field", FormFieldName: "site_key"})
		if got != "abc" {
			t.Fatalf("extractSiteKey = %q, want %q", got, "abc")
		}
	})
}

func TestSiteKeyValid(t *testing.T) {
	cases := []struct {
		name                string
		submitted, expected string
		want                bool
	}{
		{"match", "the-key", "the-key", true},
		{"mismatch", "wrong", "the-key", false},
		{"both empty", "", "", false},
		{"submitted empty", "", "the-key", false},
		{"expected empty (unset site_key can never be satisfied)", "the-key", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := siteKeyValid(c.submitted, c.expected); got != c.want {
				t.Fatalf("siteKeyValid(%q, %q) = %v, want %v", c.submitted, c.expected, got, c.want)
			}
		})
	}
}
