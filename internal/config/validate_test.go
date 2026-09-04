package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bossm8/formelay/internal/yamlutil"
)

func TestValidateForm(t *testing.T) {
	base := func() *FormConfig {
		return &FormConfig{
			ID:   "contact",
			Auth: AuthConfig{SiteKey: "somesitekey"},
			Channels: []ChannelConfig{
				{ID: "email-owner", Type: "email"},
			},
		}
	}

	t.Run("valid minimal form", func(t *testing.T) {
		if err := ValidateForm(base()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing site key", func(t *testing.T) {
		f := base()
		f.Auth.SiteKey = ""
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing auth.site_key")
		}
	})

	t.Run("form_field transport requires field name", func(t *testing.T) {
		f := base()
		f.Auth.Transport = "form_field"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing form_field_name")
		}
	})

	t.Run("duplicate channel ids", func(t *testing.T) {
		f := base()
		f.Channels = append(f.Channels, ChannelConfig{ID: "email-owner", Type: "webhook"})
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for duplicate channel id")
		}
	})

	t.Run("spam route references unknown channel", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.Route.SpamChannels = []string{"does-not-exist"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for unknown route channel id")
		}
	})

	t.Run("valid spam route", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnSpam = SpamActionRoute
		f.SpamFilter.Route.SpamChannels = []string{"email-owner"}
		f.SpamFilter.Route.SpamTemplate = "spam-review.tmpl"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid on_spam action", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnSpam = "explode"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid on_spam action")
		}
	})

	t.Run("invalid on_error action", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnError = "explode"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid on_error action")
		}
	})

	t.Run("invalid auth transport", func(t *testing.T) {
		f := base()
		f.Auth.Transport = "carrier_pigeon"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid auth.transport")
		}
	})

	t.Run("captcha enabled requires provider", func(t *testing.T) {
		f := base()
		f.Captcha.Enabled = true
		f.Captcha.SecretEnv = "SECRET"
		f.Captcha.ResponseField = "cf-turnstile-response"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing captcha.provider")
		}
	})

	t.Run("captcha enabled requires secret_env", func(t *testing.T) {
		f := base()
		f.Captcha.Enabled = true
		f.Captcha.Provider = "turnstile"
		f.Captcha.ResponseField = "cf-turnstile-response"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing captcha.secret_env")
		}
	})

	t.Run("captcha enabled requires response_field", func(t *testing.T) {
		f := base()
		f.Captcha.Enabled = true
		f.Captcha.Provider = "turnstile"
		f.Captcha.SecretEnv = "SECRET"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing captcha.response_field")
		}
	})

	t.Run("captcha invalid on_error rejected", func(t *testing.T) {
		f := base()
		f.Captcha.Enabled = true
		f.Captcha.Provider = "turnstile"
		f.Captcha.SecretEnv = "SECRET"
		f.Captcha.ResponseField = "cf-turnstile-response"
		f.Captcha.OnError = "maybe"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid captcha.on_error")
		}
	})

	t.Run("captcha disabled skips validation even with bogus fields", func(t *testing.T) {
		f := base()
		f.Captcha.Enabled = false
		f.Captcha.OnError = "maybe"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error for disabled captcha: %v", err)
		}
	})

	t.Run("channel missing id", func(t *testing.T) {
		f := base()
		f.Channels = []ChannelConfig{{Type: "email"}}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for channel missing id")
		}
	})

	t.Run("channel missing type", func(t *testing.T) {
		f := base()
		f.Channels = []ChannelConfig{{ID: "email-owner"}}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for channel missing type")
		}
	})

	t.Run("spam filter enabled requires provider type", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for missing spam_filter.provider.type")
		}
	})

	t.Run("invalid channels_required", func(t *testing.T) {
		f := base()
		f.ChannelsRequired = "most"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid channels_required")
		}
	})

	t.Run("response_mode unset defaults fine", func(t *testing.T) {
		if err := ValidateForm(base()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("response_mode sync accepted", func(t *testing.T) {
		f := base()
		f.ResponseMode = "sync"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("response_mode async accepted", func(t *testing.T) {
		f := base()
		f.ResponseMode = "async"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid response_mode rejected", func(t *testing.T) {
		f := base()
		f.ResponseMode = "eventually"
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid response_mode")
		}
	})

	t.Run("on_spam route requires spam_template", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnSpam = SpamActionRoute
		f.SpamFilter.Route.SpamChannels = []string{"email-owner"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error: on_spam=route with no spam_template must be rejected (previously silently dropped delivery)")
		}
	})

	t.Run("on_spam route with spam_template passes", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnSpam = SpamActionRoute
		f.SpamFilter.Route.SpamChannels = []string{"email-owner"}
		f.SpamFilter.Route.SpamTemplate = "spam-review.tmpl"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("on_error route requires error_template or spam_template fallback", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnError = SpamActionRoute
		f.SpamFilter.Route.ErrorChannels = []string{"email-owner"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error: on_error=route with neither error_template nor spam_template must be rejected")
		}
	})

	t.Run("on_error route falls back to spam_template", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnError = SpamActionRoute
		f.SpamFilter.Route.ErrorChannels = []string{"email-owner"}
		f.SpamFilter.Route.SpamTemplate = "spam-review.tmpl"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: on_error=route should accept spam_template as a fallback: %v", err)
		}
	})

	t.Run("on_error route with its own error_template passes", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.OnError = SpamActionRoute
		f.SpamFilter.Route.ErrorChannels = []string{"email-owner"}
		f.SpamFilter.Route.ErrorTemplate = "error-review.tmpl"
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("channel rate_limit: valid block passes", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("channel rate_limit: invalid on_limit rejected", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10, OnLimit: "retry"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid rate_limit.on_limit")
		}
	})

	t.Run("channel rate_limit: non-positive rate rejected", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{Rate: 0, Window: yamlutil.Duration(time.Minute), Burst: 10}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for rate_limit.rate <= 0")
		}
	})

	t.Run("channel rate_limit: zero window rejected", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{Rate: 10, Burst: 10}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for rate_limit.window <= 0")
		}
	})

	t.Run("channel rate_limit: non-positive burst rejected", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 0}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for rate_limit.burst <= 0")
		}
	})

	t.Run("spam_filter rate_limit: valid block passes", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.RateLimit = &OutboundRateLimitConfig{Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("spam_filter rate_limit: invalid on_limit rejected", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.RateLimit = &OutboundRateLimitConfig{Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10, OnLimit: "retry"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid spam_filter.rate_limit.on_limit")
		}
	})

	t.Run("spam_filter rate_limit: non-positive rate rejected", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.RateLimit = &OutboundRateLimitConfig{Rate: 0, Window: yamlutil.Duration(time.Minute), Burst: 10}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for spam_filter.rate_limit.rate <= 0")
		}
	})

	t.Run("channel rate_limit: shared_key alone passes", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{SharedKey: "primary-smtp"}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("channel rate_limit: shared_key with inline rate is rejected as mutually exclusive", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{SharedKey: "primary-smtp", Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for shared_key combined with inline rate/window/burst")
		}
	})

	t.Run("channel rate_limit: shared_key with inline on_limit is rejected as mutually exclusive", func(t *testing.T) {
		f := base()
		f.Channels[0].RateLimit = &OutboundRateLimitConfig{SharedKey: "primary-smtp", OnLimit: "fail"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for shared_key combined with inline on_limit")
		}
	})

	t.Run("spam_filter rate_limit: shared_key alone passes", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.RateLimit = &OutboundRateLimitConfig{SharedKey: "ai-provider"}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("spam_filter rate_limit: shared_key with inline burst is rejected as mutually exclusive", func(t *testing.T) {
		f := base()
		f.SpamFilter.Enabled = true
		f.SpamFilter.Provider.Type = "ai"
		f.SpamFilter.RateLimit = &OutboundRateLimitConfig{SharedKey: "ai-provider", Burst: 5}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for shared_key combined with inline burst")
		}
	})

	t.Run("fields.validators: built-in kinds accepted", func(t *testing.T) {
		f := base()
		f.Fields.Validators = map[string]string{"a": "email", "b": "url", "c": "notblank"}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fields.validators: valid regex accepted", func(t *testing.T) {
		f := base()
		f.Fields.Validators = map[string]string{"zip": `regex:^\d{5}$`}
		if err := ValidateForm(f); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fields.validators: invalid regex syntax rejected", func(t *testing.T) {
		f := base()
		f.Fields.Validators = map[string]string{"zip": "regex:^[unterminated"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for invalid regex syntax")
		}
	})

	t.Run("fields.validators: unknown kind rejected (previously silently no-op'd)", func(t *testing.T) {
		f := base()
		f.Fields.Validators = map[string]string{"email": "emial"}
		if err := ValidateForm(f); err == nil {
			t.Fatal("expected error for unrecognized validator kind")
		}
	})
}

func TestValidateGlobal(t *testing.T) {
	t.Run("valid defaults", func(t *testing.T) {
		g := DefaultGlobalConfig()
		if err := ValidateGlobal(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("defaults populate a non-empty internal.listen_addr", func(t *testing.T) {
		g := DefaultGlobalConfig()
		if g.Internal.ListenAddr == "" {
			t.Fatal("expected a non-empty internal.listen_addr by default")
		}
	})

	t.Run("empty internal.listen_addr rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Internal.ListenAddr = ""
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for empty internal.listen_addr")
		}
	})

	t.Run("defaults enable the HTTP reload trigger with a non-empty path", func(t *testing.T) {
		g := DefaultGlobalConfig()
		if !g.Reload.HandleHTTP || g.Reload.HTTPPath == "" {
			t.Fatalf("expected reload.handle_http=true and a non-empty http_path by default, got %+v", g.Reload)
		}
	})

	t.Run("handle_http with an empty http_path is rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Reload.HandleHTTP = true
		g.Reload.HTTPPath = ""
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for reload.handle_http=true with an empty http_path")
		}
	})

	t.Run("handle_http disabled tolerates an empty http_path", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Reload.HandleHTTP = false
		g.Reload.HTTPPath = ""
		if err := ValidateGlobal(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valkey backend requires addresses", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.Backend = "valkey"
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for missing valkey addresses")
		}
	})

	t.Run("unknown backend rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.Backend = "sqlite"
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for unknown backend")
		}
	})

	t.Run("tls enabled requires cert and key paths", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Server.TLS.Enabled = true
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for missing cert_file/key_file")
		}
	})

	t.Run("tls enabled requires the cert and key files to actually exist", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Server.TLS.Enabled = true
		g.Server.TLS.CertFile = "/nonexistent/cert.pem"
		g.Server.TLS.KeyFile = "/nonexistent/key.pem"
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for nonexistent cert/key files")
		}
	})

	t.Run("empty listen_addr rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.Server.ListenAddr = ""
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for empty server.listen_addr")
		}
	})

	t.Run("empty forms_dir rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.FormsDir = ""
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for empty forms_dir")
		}
	})

	t.Run("valkey invalid on_error rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.Backend = "valkey"
		g.RateLimit.Valkey.Addresses = []string{"127.0.0.1:6379"}
		g.RateLimit.Valkey.OnError = "maybe"
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for invalid rate_limit.valkey.on_error")
		}
	})

	t.Run("valkey password_env set but env var unset is rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.Backend = "valkey"
		g.RateLimit.Valkey.Addresses = []string{"127.0.0.1:6379"}
		g.RateLimit.Valkey.PasswordEnv = "TEST_VALKEY_UNSET_PASSWORD_VAR"
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for rate_limit.valkey.password_env referencing an unset env var")
		}
	})

	t.Run("outbound_buckets: valid entry passes", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.OutboundBuckets = map[string]OutboundBucketConfig{
			"primary-smtp": {Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10},
		}
		if err := ValidateGlobal(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("outbound_buckets: invalid on_limit rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.OutboundBuckets = map[string]OutboundBucketConfig{
			"primary-smtp": {Rate: 10, Window: yamlutil.Duration(time.Minute), Burst: 10, OnLimit: "retry"},
		}
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for invalid outbound_buckets[...].on_limit")
		}
	})

	t.Run("outbound_buckets: non-positive rate rejected", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.OutboundBuckets = map[string]OutboundBucketConfig{
			"primary-smtp": {Rate: 0, Window: yamlutil.Duration(time.Minute), Burst: 10},
		}
		if err := ValidateGlobal(g); err == nil {
			t.Fatal("expected error for outbound_buckets[...].rate <= 0")
		}
	})

	t.Run("valkey empty on_error is left unset by config layer, not defaulted to allow", func(t *testing.T) {
		g := DefaultGlobalConfig()
		g.RateLimit.Backend = "valkey"
		g.RateLimit.Valkey.Addresses = []string{"127.0.0.1:6379"}
		if err := ValidateGlobal(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.RateLimit.Valkey.OnError != "" {
			t.Fatalf("ValidateGlobal must not rewrite on_error; the \"allow\" default is applied in internal/ratelimit/valkey, got %q", g.RateLimit.Valkey.OnError)
		}
	})

	t.Run("tls with real files passes", func(t *testing.T) {
		dir := t.TempDir()
		cert := filepath.Join(dir, "cert.pem")
		key := filepath.Join(dir, "key.pem")
		if err := os.WriteFile(cert, []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(key, []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
		g := DefaultGlobalConfig()
		g.Server.TLS.Enabled = true
		g.Server.TLS.CertFile = cert
		g.Server.TLS.KeyFile = key
		if err := ValidateGlobal(g); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
