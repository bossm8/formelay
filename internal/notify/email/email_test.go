package email

import (
	"context"
	"testing"

	"github.com/bossm8/formelay/internal/notify"
)

func TestConfigValidate(t *testing.T) {
	valid := func() Config {
		return Config{To: []string{"owner@example.com"}, Host: "smtp.example.com", From: "no-reply@example.com", BodyType: "html", BodyInline: "hi"}
	}
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(*Config) {}, false},
		{"missing to", func(c *Config) { c.To = nil }, true},
		{"missing host", func(c *Config) { c.Host = "" }, true},
		{"missing from", func(c *Config) { c.From = "" }, true},
		{"invalid body_type", func(c *Config) { c.BodyType = "markdown" }, true},
		{"missing body template and inline", func(c *Config) { c.BodyInline = ""; c.BodyTemplate = "" }, true},
		{"body_template alone (no inline) is sufficient", func(c *Config) { c.BodyInline = ""; c.BodyTemplate = "body.tmpl" }, false},
		{"password_env set but the env var is unset", func(c *Config) { c.PasswordEnv = "TEST_EMAIL_UNSET_PASSWORD_VAR" }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := valid()
			c.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

// TestSendRejectsBadAddressesBeforeDialingSMTP covers the error paths that
// return before any real SMTP connection is attempted, so these run without
// a live/fake SMTP server.
func TestSendRejectsBadAddressesBeforeDialingSMTP(t *testing.T) {
	t.Run("invalid From address", func(t *testing.T) {
		n := &emailNotifier{cfg: Config{From: "not-an-address", To: []string{"owner@example.com"}}}
		if err := n.Send(context.Background(), notify.RenderedMessage{}); err == nil {
			t.Fatal("expected an error for an invalid From address")
		}
	})

	t.Run("invalid To address", func(t *testing.T) {
		n := &emailNotifier{cfg: Config{From: "no-reply@example.com", To: []string{"not-an-address"}}}
		if err := n.Send(context.Background(), notify.RenderedMessage{}); err == nil {
			t.Fatal("expected an error for an invalid To address")
		}
	})

	t.Run("reply-to header injection attempt is rejected, not sent", func(t *testing.T) {
		n := &emailNotifier{cfg: Config{From: "no-reply@example.com", To: []string{"owner@example.com"}}}
		msg := notify.RenderedMessage{Meta: map[string]string{"reply_to": "victim@example.com\r\nBcc: attacker@evil.example"}}
		if err := n.Send(context.Background(), msg); err == nil {
			t.Fatal("expected a header-injection attempt in reply_to to be rejected")
		}
	})
}

func TestReplyToField(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"unset", Config{}, ""},
		{"set", Config{ReplyToField: "message"}, "message"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := &emailNotifier{cfg: c.cfg}
			if got := n.ReplyToField(); got != c.want {
				t.Fatalf("ReplyToField() = %q, want %q", got, c.want)
			}
		})
	}
}
