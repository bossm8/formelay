package captcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/yamlutil"
)

// Config.Validate() requires an https verify_url (matching real providers),
// so these tests construct genericVerifier directly against an httptest
// server rather than going through NewFactory/Validate.

func TestGenericVerifierPresetShape(t *testing.T) {
	os.Setenv("TEST_CAPTCHA_SECRET", "s3cret")
	defer os.Unsetenv("TEST_CAPTCHA_SECRET")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("server: parse form: %v", err)
		}
		if r.PostForm.Get("secret") != "s3cret" {
			t.Fatalf("expected secret to be forwarded, got %q", r.PostForm.Get("secret"))
		}
		json.NewEncoder(w).Encode(map[string]any{"success": r.PostForm.Get("response") == "good-token"})
	}))
	defer srv.Close()

	p := presets["turnstile"]
	cfg := Config{
		SecretEnv: "TEST_CAPTCHA_SECRET", VerifyURL: srv.URL,
		RequestEncoding: p.RequestEncoding, SecretParam: p.SecretParam,
		ResponseParam: p.ResponseParam, RemoteIPParam: p.RemoteIPParam, SuccessField: p.SuccessField,
	}
	v := &genericVerifier{cfg: cfg, client: srv.Client()}

	ok, err := v.Verify(context.Background(), "good-token", "1.2.3.4")
	if err != nil || !ok {
		t.Fatalf("expected verify to succeed: ok=%v err=%v", ok, err)
	}
	ok, err = v.Verify(context.Background(), "bad-token", "1.2.3.4")
	if err != nil || ok {
		t.Fatalf("expected verify to fail for wrong token: ok=%v err=%v", ok, err)
	}
}

// TestNewFactoryPresetSurvivesFullConfigRoundTrip is a regression test for a
// bug where preset defaults (e.g. Turnstile's verify_url) were clobbered:
// the orchestration layer (internal/app) builds the raw map passed to
// Registry.Build by marshaling the *entire* config.CaptchaConfig struct via
// yamlutil.ToMap — including every field an operator left unset, as an
// explicit empty string, not an absent key. NewFactory used to apply the
// preset defaults *before* decoding that map, so those explicit empty
// strings silently overwrote them, and `provider: turnstile` with no
// verify_url override failed validation instead of using the preset. Fixed
// by applying preset defaults only to fields still zero *after* decoding.
func TestNewFactoryPresetSurvivesFullConfigRoundTrip(t *testing.T) {
	os.Setenv("TEST_CAPTCHA_PRESET_SECRET", "s3cret")
	defer os.Unsetenv("TEST_CAPTCHA_PRESET_SECRET")

	// Exactly what an operator's forms.d/*.yaml produces: only enabled,
	// provider, secret_env, and response_field set — everything else
	// (verify_url, request_encoding, ...) is a config.CaptchaConfig zero
	// value, which yamlutil.ToMap still serializes explicitly.
	cc := config.CaptchaConfig{
		Enabled:       true,
		Provider:      "turnstile",
		SecretEnv:     "TEST_CAPTCHA_PRESET_SECRET",
		ResponseField: "cf-turnstile-response",
	}
	raw, err := yamlutil.ToMap(cc)
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}

	v, err := NewFactory("turnstile")(raw)
	if err != nil {
		t.Fatalf("expected the turnstile preset's verify_url to fill the gap, got error: %v", err)
	}
	gv, ok := v.(*genericVerifier)
	if !ok {
		t.Fatalf("expected *genericVerifier, got %T", v)
	}
	if gv.cfg.VerifyURL != presets["turnstile"].VerifyURL {
		t.Fatalf("verify_url = %q, want the turnstile preset's %q", gv.cfg.VerifyURL, presets["turnstile"].VerifyURL)
	}
}

func TestGenericVerifierScoreThreshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "score": 0.3})
	}))
	defer srv.Close()

	cfg := Config{
		VerifyURL: srv.URL, RequestEncoding: "form",
		SecretParam: "secret", ResponseParam: "response", SuccessField: "success",
		ScoreField: "score", MinScore: 0.5,
	}
	v := &genericVerifier{cfg: cfg, client: srv.Client()}
	ok, err := v.Verify(context.Background(), "tok", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected verify to fail when score below min_score")
	}
}

func TestGenericVerifierJSONEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected JSON content type, got %q", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("server: decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": body["response"] == "ok"})
	}))
	defer srv.Close()

	cfg := Config{
		VerifyURL: srv.URL, RequestEncoding: "json",
		SecretParam: "secret", ResponseParam: "response", SuccessField: "success",
	}
	v := &genericVerifier{cfg: cfg, client: srv.Client()}
	ok, err := v.Verify(context.Background(), "ok", "")
	if err != nil || !ok {
		t.Fatalf("expected verify to succeed: ok=%v err=%v", ok, err)
	}
}

func TestGenericVerifierValidate(t *testing.T) {
	os.Setenv("TEST_CAPTCHA_SECRET3", "s3cret")
	defer os.Unsetenv("TEST_CAPTCHA_SECRET3")

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid", Config{SecretEnv: "TEST_CAPTCHA_SECRET3", VerifyURL: "https://example.com/verify", RequestEncoding: "form", SuccessField: "success"}, false},
		{"http rejected", Config{SecretEnv: "TEST_CAPTCHA_SECRET3", VerifyURL: "http://example.com/verify", RequestEncoding: "form", SuccessField: "success"}, true},
		{"missing secret_env", Config{VerifyURL: "https://example.com/verify", RequestEncoding: "form", SuccessField: "success"}, true},
		{"unset env var", Config{SecretEnv: "TEST_CAPTCHA_UNSET_XYZ", VerifyURL: "https://example.com/verify", RequestEncoding: "form", SuccessField: "success"}, true},
		{"bad encoding", Config{SecretEnv: "TEST_CAPTCHA_SECRET3", VerifyURL: "https://example.com/verify", RequestEncoding: "xml", SuccessField: "success"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v got err=%v", c.wantErr, err)
			}
		})
	}
}
