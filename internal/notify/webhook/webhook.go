// Package webhook implements the generic outbound-webhook Notifier: it POSTs
// the already-rendered payload to any operator-configured URL. This is the
// zero-code integration point for Slack/Telegram/PagerDuty/etc. via their
// own incoming webhooks.
package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/yamlutil"
)

const Type = "webhook"

type AuthConfig struct {
	Type        string `yaml:"type"` // none | basic | bearer
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
	TokenEnv    string `yaml:"token_env"`
}

type Config struct {
	URL            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	Auth           AuthConfig        `yaml:"auth"`
	Timeout        yamlutil.Duration `yaml:"timeout"`
	Template       string            `yaml:"template"`
	TemplateInline string            `yaml:"template_inline"`
}

func (c *Config) Validate() error {
	if c.URL == "" {
		return errors.New("webhook: 'url' is required")
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("webhook: 'url' must be a valid https URL")
	}
	switch c.Auth.Type {
	case "", "none":
	case "basic":
		if c.Auth.Username == "" || c.Auth.PasswordEnv == "" {
			return errors.New("webhook: basic auth requires 'username' and 'password_env'")
		}
	case "bearer":
		if c.Auth.TokenEnv == "" {
			return errors.New("webhook: bearer auth requires 'token_env'")
		}
	default:
		return fmt.Errorf("webhook: unknown auth type %q", c.Auth.Type)
	}
	return nil
}

type webhookNotifier struct {
	cfg    Config
	client *http.Client
}

// New is a notify.NewNotifierFunc.
func New(raw map[string]any, _ notify.GlobalDefaults) (notify.Notifier, error) {
	var cfg Config
	cfg.Method = http.MethodPost
	cfg.Timeout = yamlutil.Duration(5 * time.Second)
	if err := yamlutil.Decode(raw, &cfg); err != nil {
		return nil, fmt.Errorf("webhook: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &webhookNotifier{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout.Std()}}, nil
}

func (w *webhookNotifier) Type() string { return Type }

func (w *webhookNotifier) TemplateRefs() []notify.TemplateRef {
	return []notify.TemplateRef{{
		Key:         "body",
		Kind:        render.KindText,
		Path:        w.cfg.Template,
		Inline:      w.cfg.TemplateInline,
		ContentType: "application/json",
	}}
}

func (w *webhookNotifier) Send(ctx context.Context, msg notify.RenderedMessage) error {
	req, err := http.NewRequestWithContext(ctx, w.cfg.Method, w.cfg.URL, bytes.NewReader(msg.Body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	contentType := msg.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}
	switch w.cfg.Auth.Type {
	case "basic":
		req.SetBasicAuth(w.cfg.Auth.Username, os.Getenv(w.cfg.Auth.PasswordEnv))
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+os.Getenv(w.cfg.Auth.TokenEnv))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
