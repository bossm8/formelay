package api

import (
	"strings"
	"testing"

	"github.com/bossm8/formelay/internal/config"
)

func TestSanitizeFields(t *testing.T) {
	t.Run("delegates per-field cleanup, using the configured max length", func(t *testing.T) {
		out, err := sanitizeFields(map[string]string{"name": "Alice"}, config.FieldsConfig{MaxFieldLength: 3})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := out["name"]; got != "Ali" {
			t.Fatalf("name = %q, want truncated to 3 runes", got)
		}
	})

	t.Run("zero/unset max length falls back to the package default", func(t *testing.T) {
		long := strings.Repeat("a", defaultMaxFieldLength+10)
		out, err := sanitizeFields(map[string]string{"name": long}, config.FieldsConfig{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out["name"]) != defaultMaxFieldLength {
			t.Fatalf("name length = %d, want default cap %d", len(out["name"]), defaultMaxFieldLength)
		}
	})

	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		_, err := sanitizeFields(map[string]string{"name": "\xff\xfe"}, config.FieldsConfig{})
		if err == nil {
			t.Fatal("expected an error for invalid UTF-8")
		}
	})
}

func TestValidateFields(t *testing.T) {
	cfg := config.FieldsConfig{
		Required:   []string{"name", "email"},
		Validators: map[string]string{"email": "email"},
	}

	t.Run("all required fields present and valid", func(t *testing.T) {
		failed := validateFields(map[string]string{"name": "Alice", "email": "alice@example.com"}, cfg)
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %v", failed)
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		failed := validateFields(map[string]string{"name": "Alice"}, cfg)
		if len(failed) != 1 || failed[0] != "email" {
			t.Fatalf("expected [email] to fail as missing, got %v", failed)
		}
	})

	t.Run("present but invalid per its validator", func(t *testing.T) {
		failed := validateFields(map[string]string{"name": "Alice", "email": "not-an-email"}, cfg)
		if len(failed) != 1 || failed[0] != "email" {
			t.Fatalf("expected [email] to fail validation, got %v", failed)
		}
	})
}

func TestRunValidator(t *testing.T) {
	cases := []struct {
		kind, value string
		want        bool
	}{
		{"email", "alice@example.com", true},
		{"email", "not-an-email", false},
		{"url", "https://example.com", true},
		{"url", "not a url", false},
		{"url", "/relative/path", false}, // no scheme/host
		{"notblank", "something", true},
		{"notblank", "", false},
		{"unknown-validator-name", "anything", true}, // unknown validator passes through
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.value, func(t *testing.T) {
			if got := runValidator(c.kind, c.value); got != c.want {
				t.Fatalf("runValidator(%q, %q) = %v, want %v", c.kind, c.value, got, c.want)
			}
		})
	}
}
