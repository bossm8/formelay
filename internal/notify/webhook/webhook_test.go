package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bossm8/formelay/internal/notify"
)

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"missing url", Config{}, true},
		{"non-https url rejected", Config{URL: "http://example.com/hook"}, true},
		{"valid, no auth", Config{URL: "https://example.com/hook"}, false},
		{"auth none explicit", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "none"}}, false},
		{"basic requires username", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "basic", PasswordEnv: "P"}}, true},
		{"basic requires password_env", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "basic", Username: "u"}}, true},
		{"basic valid", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "basic", Username: "u", PasswordEnv: "P"}}, false},
		{"bearer requires token_env", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "bearer"}}, true},
		{"bearer valid", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "bearer", TokenEnv: "T"}}, false},
		{"unknown auth type rejected", Config{URL: "https://example.com/hook", Auth: AuthConfig{Type: "carrier_pigeon"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestSend(t *testing.T) {
	t.Run("method, headers, and body reach the server", func(t *testing.T) {
		var gotMethod, gotBody, gotContentType, gotCustomHeader string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotContentType = r.Header.Get("Content-Type")
			gotCustomHeader = r.Header.Get("X-Custom")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		n := &webhookNotifier{
			cfg:    Config{URL: srv.URL, Method: http.MethodPost, Headers: map[string]string{"X-Custom": "yes"}},
			client: srv.Client(),
		}
		err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{"ok":true}`), ContentType: "application/json"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("method = %q, want POST", gotMethod)
		}
		if gotContentType != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", gotContentType)
		}
		if gotCustomHeader != "yes" {
			t.Fatalf("X-Custom header = %q, want yes", gotCustomHeader)
		}
		if gotBody != `{"ok":true}` {
			t.Fatalf("body = %q", gotBody)
		}
	})

	t.Run("empty message content type defaults to application/json", func(t *testing.T) {
		var gotContentType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		n := &webhookNotifier{cfg: Config{URL: srv.URL, Method: http.MethodPost}, client: srv.Client()}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotContentType != "application/json" {
			t.Fatalf("Content-Type = %q, want default application/json", gotContentType)
		}
	})

	t.Run("operator-configured Headers take precedence over the default Content-Type", func(t *testing.T) {
		var gotContentType string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		n := &webhookNotifier{
			cfg:    Config{URL: srv.URL, Method: http.MethodPost, Headers: map[string]string{"Content-Type": "text/plain"}},
			client: srv.Client(),
		}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`plain text`), ContentType: "application/json"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotContentType != "text/plain" {
			t.Fatalf("Content-Type = %q, want the operator-configured header to win over msg.ContentType", gotContentType)
		}
	})

	t.Run("basic auth sets the Authorization header correctly", func(t *testing.T) {
		var gotUser, gotPass string
		var gotOK bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotPass, gotOK = r.BasicAuth()
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		t.Setenv("TEST_WEBHOOK_BASIC_PASS", "s3cret")

		n := &webhookNotifier{
			cfg: Config{URL: srv.URL, Method: http.MethodPost,
				Auth: AuthConfig{Type: "basic", Username: "alice", PasswordEnv: "TEST_WEBHOOK_BASIC_PASS"}},
			client: srv.Client(),
		}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !gotOK || gotUser != "alice" || gotPass != "s3cret" {
			t.Fatalf("BasicAuth() = (%q, %q, %v), want (alice, s3cret, true)", gotUser, gotPass, gotOK)
		}
	})

	t.Run("bearer auth sets the Authorization header correctly", func(t *testing.T) {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		t.Setenv("TEST_WEBHOOK_BEARER_TOKEN", "tok123")

		n := &webhookNotifier{
			cfg:    Config{URL: srv.URL, Method: http.MethodPost, Auth: AuthConfig{Type: "bearer", TokenEnv: "TEST_WEBHOOK_BEARER_TOKEN"}},
			client: srv.Client(),
		}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotAuth != "Bearer tok123" {
			t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer tok123")
		}
	})

	t.Run("non-2xx status is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		n := &webhookNotifier{cfg: Config{URL: srv.URL, Method: http.MethodPost}, client: srv.Client()}
		if err := n.Send(context.Background(), notify.RenderedMessage{Body: []byte(`{}`)}); err == nil {
			t.Fatal("expected an error for a non-2xx response")
		}
	})
}
