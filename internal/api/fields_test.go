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
		loud, silent := validateFields(map[string]string{"name": "Alice", "email": "alice@example.com"}, cfg)
		if len(loud) != 0 || len(silent) != 0 {
			t.Fatalf("expected no failures, got loud=%v silent=%v", loud, silent)
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		loud, silent := validateFields(map[string]string{"name": "Alice"}, cfg)
		if len(loud) != 1 || loud[0] != "email" {
			t.Fatalf("expected [email] to fail as missing (loud), got loud=%v", loud)
		}
		if len(silent) != 0 {
			t.Fatalf("a missing required field is never silent, got %v", silent)
		}
	})

	t.Run("present but invalid per its validator", func(t *testing.T) {
		loud, silent := validateFields(map[string]string{"name": "Alice", "email": "not-an-email"}, cfg)
		if len(loud) != 1 || loud[0] != "email" {
			t.Fatalf("expected [email] to fail validation (loud), got loud=%v", loud)
		}
		if len(silent) != 0 {
			t.Fatalf("expected no silent failures, got %v", silent)
		}
	})

	t.Run("a silent:-marked validator failure lands in silent, not loud", func(t *testing.T) {
		silentCfg := config.FieldsConfig{Validators: map[string]string{"reply_to": "silent:not:regex:@formtests\\.info$"}}
		loud, silent := validateFields(map[string]string{"reply_to": "bot@formtests.info"}, silentCfg)
		if len(loud) != 0 {
			t.Fatalf("expected no loud failures, got %v", loud)
		}
		if len(silent) != 1 || silent[0] != "reply_to" {
			t.Fatalf("expected [reply_to] to fail silently, got %v", silent)
		}
	})

	t.Run("the same denylist without silent: is an ordinary loud failure", func(t *testing.T) {
		loudCfg := config.FieldsConfig{Validators: map[string]string{"reply_to": "not:regex:@formtests\\.info$"}}
		loud, silent := validateFields(map[string]string{"reply_to": "bot@formtests.info"}, loudCfg)
		if len(silent) != 0 {
			t.Fatalf("expected no silent failures, got %v", silent)
		}
		if len(loud) != 1 || loud[0] != "reply_to" {
			t.Fatalf("expected [reply_to] to fail loudly, got %v", loud)
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
		{`regex:^\d{5}$`, "12345", true},
		{`regex:^\d{5}$`, "1234", false},
		{`regex:^\d{5}$`, "abcde", false},
		{"regex:hello", "say hello there", true}, // no anchors: plain substring match, as documented
		{"regex:[", "anything", false},           // invalid pattern fails closed, doesn't panic

		// not: inverts the underlying kind's result — a denylist, using
		// the existing regex engine
		{`not:regex:@formtests\.info$`, "bot@formtests.info", false}, // matches -> fails (blocked)
		{`not:regex:@formtests\.info$`, "alice@example.com", true},   // doesn't match -> passes
		{"not:notblank", "", true},                                   // notblank("") is false, negated -> true
		{"not:notblank", "something", false},

		// silent: is transparent to the check itself — same pass/fail as
		// the unmarked kind; only how the caller *reports* a failure
		// differs (see TestValidateFields).
		{"silent:notblank", "", false},
		{"silent:notblank", "something", true},

		// stacked, in either order — stripping is order-independent, so
		// both must produce the same result.
		{`silent:not:regex:@formtests\.info$`, "bot@formtests.info", false},
		{`not:silent:regex:@formtests\.info$`, "bot@formtests.info", false},
	}
	for _, c := range cases {
		t.Run(c.kind+"/"+c.value, func(t *testing.T) {
			if got := runValidator(c.kind, c.value); got != c.want {
				t.Fatalf("runValidator(%q, %q) = %v, want %v", c.kind, c.value, got, c.want)
			}
		})
	}
}
