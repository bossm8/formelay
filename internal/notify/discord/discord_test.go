package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bossm8/formelay/internal/notify"
)

func TestConfigValidate(t *testing.T) {
	t.Run("missing webhook_url_env is rejected", func(t *testing.T) {
		if err := (&Config{}).Validate(); err == nil {
			t.Fatal("expected error for missing webhook_url_env")
		}
	})

	t.Run("env var referenced by webhook_url_env not set is rejected", func(t *testing.T) {
		os.Unsetenv("TEST_DISCORD_UNSET_VAR")
		cfg := &Config{WebhookURLEnv: "TEST_DISCORD_UNSET_VAR"}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for an unset env var")
		}
	})

	t.Run("valid", func(t *testing.T) {
		t.Setenv("TEST_DISCORD_WEBHOOK_ENV", "https://discord.example/webhook")
		cfg := &Config{WebhookURLEnv: "TEST_DISCORD_WEBHOOK_ENV"}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSend(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()
		t.Setenv("TEST_DISCORD_SEND_URL", srv.URL)

		n := &discordNotifier{cfg: Config{WebhookURLEnv: "TEST_DISCORD_SEND_URL"}, client: srv.Client()}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{"content":"hi"}`)}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-2xx status is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()
		t.Setenv("TEST_DISCORD_SEND_URL_FAIL", srv.URL)

		n := &discordNotifier{cfg: Config{WebhookURLEnv: "TEST_DISCORD_SEND_URL_FAIL"}, client: srv.Client()}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err == nil {
			t.Fatal("expected an error for a non-2xx response")
		}
	})

	t.Run("env var read fresh at send time, not cached from construction", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		n := &discordNotifier{cfg: Config{WebhookURLEnv: "TEST_DISCORD_SEND_URL_LATE"}, client: srv.Client()}

		os.Unsetenv("TEST_DISCORD_SEND_URL_LATE")
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err == nil {
			t.Fatal("expected an error while the env var is unset")
		}

		t.Setenv("TEST_DISCORD_SEND_URL_LATE", srv.URL)
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err != nil {
			t.Fatalf("expected Send to pick up the env var set after construction, got: %v", err)
		}
	})
}
