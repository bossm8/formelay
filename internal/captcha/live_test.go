//go:build live

// Exercises the real Verify() code path against each provider's actual
// verify endpoint, using their official public "always passes"/"always
// fails" test key pairs (published specifically for automated testing,
// see e.g. https://developers.cloudflare.com/turnstile/troubleshooting/testing/
// and https://docs.hcaptcha.com/#integration-testing-test-key). These are
// not our own httptest fakes: a pass here proves the real request shape
// (field names, encoding, response parsing) matches what the provider
// actually expects, not just what we assumed it expects.
//
// Run with: go test -tags=live ./internal/captcha/... -v
// or:       make test-live
package captcha

import (
	"context"
	"os"
	"testing"
)

const (
	turnstileTestSecretPass = "1x0000000000000000000000000000000AA"
	turnstileTestSecretFail = "2x0000000000000000000000000000000AA"
	turnstileTestResponse   = "XXXX.DUMMY.TOKEN.XXXX"
	turnstileVerifyURL      = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	hcaptchaTestSecret      = "0x0000000000000000000000000000000000000000"
	hcaptchaTestResponse    = "10000000-aaaa-bbbb-cccc-000000000001"
)

func setSecretEnv(t *testing.T, name, value string) {
	t.Helper()
	old, had := os.LookupEnv(name)
	os.Setenv(name, value)
	t.Cleanup(func() {
		if had {
			os.Setenv(name, old)
		} else {
			os.Unsetenv(name)
		}
	})
}

func TestLiveTurnstilePreset(t *testing.T) {
	setSecretEnv(t, "TURNSTILE_LIVE_TEST_SECRET", turnstileTestSecretPass)
	v, err := NewFactory("turnstile")(map[string]any{"secret_env": "TURNSTILE_LIVE_TEST_SECRET"})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	ok, err := v.Verify(context.Background(), turnstileTestResponse, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected the always-pass test key pair to verify successfully")
	}
}

func TestLiveTurnstileAlwaysFailsSecret(t *testing.T) {
	// The counterpart to the always-pass case above: proves we correctly
	// surface a real `success: false` from the provider, not just that we
	// can parse a `true`.
	setSecretEnv(t, "TURNSTILE_LIVE_TEST_SECRET_FAIL", turnstileTestSecretFail)
	v, err := NewFactory("turnstile")(map[string]any{"secret_env": "TURNSTILE_LIVE_TEST_SECRET_FAIL"})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	ok, err := v.Verify(context.Background(), turnstileTestResponse, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected the always-fail test secret to verify as failed")
	}
}

func TestLiveHCaptchaPreset(t *testing.T) {
	setSecretEnv(t, "HCAPTCHA_LIVE_TEST_SECRET", hcaptchaTestSecret)
	v, err := NewFactory("hcaptcha")(map[string]any{"secret_env": "HCAPTCHA_LIVE_TEST_SECRET"})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	ok, err := v.Verify(context.Background(), hcaptchaTestResponse, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected the always-pass test key pair to verify successfully")
	}
}

// TestLiveGenericProvider proves the `provider: generic` escape hatch (no
// named preset, every field set explicitly by the operator) against a real
// server, not just a preset's pre-filled defaults. It reuses Turnstile's
// real endpoint as that real server, since Turnstile's protocol is exactly
// the shape `provider: generic` targets, but deliberately configures it as
// if no preset existed, the same way an operator would for a provider we
// don't ship a preset for.
func TestLiveGenericProvider(t *testing.T) {
	setSecretEnv(t, "GENERIC_LIVE_TEST_SECRET", turnstileTestSecretPass)
	v, err := NewFactory("")(map[string]any{
		"secret_env":       "GENERIC_LIVE_TEST_SECRET",
		"verify_url":       turnstileVerifyURL,
		"request_encoding": "form",
		"secret_param":     "secret",
		"response_param":   "response",
		"remoteip_param":   "remoteip",
		"success_field":    "success",
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	if v.Type() != "generic" {
		t.Fatalf("expected Type() = %q, got %q", "generic", v.Type())
	}
	ok, err := v.Verify(context.Background(), turnstileTestResponse, "203.0.113.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected the manually-configured generic provider to verify successfully")
	}
}
