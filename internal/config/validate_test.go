package config

import "testing"

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
}

func TestValidateGlobal(t *testing.T) {
	t.Run("valid defaults", func(t *testing.T) {
		g := DefaultGlobalConfig()
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
}
