// Package discord implements the Notifier for Discord incoming webhooks.
package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/yamlutil"
)

const Type = "discord"

type Config struct {
	WebhookURLEnv  string            `yaml:"webhook_url_env"`
	Timeout        yamlutil.Duration `yaml:"timeout"`
	Template       string            `yaml:"template"`
	TemplateInline string            `yaml:"template_inline"`
}

func (c *Config) Validate() error {
	if c.WebhookURLEnv == "" {
		return errors.New("discord: 'webhook_url_env' is required")
	}
	if os.Getenv(c.WebhookURLEnv) == "" {
		return fmt.Errorf("discord: environment variable %q referenced by 'webhook_url_env' is not set", c.WebhookURLEnv)
	}
	return nil
}

type discordNotifier struct {
	cfg    Config
	client *http.Client
}

// New is a notify.NewNotifierFunc. The rendered message body is expected to
// already be a complete Discord webhook JSON payload (see the `discord`
// template kind), produced via render.KindText + the `json` FuncMap entry.
func New(raw map[string]any, _ notify.GlobalDefaults) (notify.Notifier, error) {
	var cfg Config
	cfg.Timeout = yamlutil.Duration(5 * time.Second)
	if err := yamlutil.Decode(raw, &cfg); err != nil {
		return nil, fmt.Errorf("discord: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &discordNotifier{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout.Std()}}, nil
}

func (d *discordNotifier) Type() string { return Type }

func (d *discordNotifier) TemplateRefs() []notify.TemplateRef {
	return []notify.TemplateRef{{
		Key:         "body",
		Kind:        render.KindText,
		Path:        d.cfg.Template,
		Inline:      d.cfg.TemplateInline,
		ContentType: "application/json",
	}}
}

func (d *discordNotifier) Send(ctx context.Context, msg notify.RenderedMessage) error {
	webhookURL := os.Getenv(d.cfg.WebhookURLEnv)
	if webhookURL == "" {
		return fmt.Errorf("discord: environment variable %q is empty", d.cfg.WebhookURLEnv)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(msg.Body))
	if err != nil {
		return fmt.Errorf("discord: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord: unexpected status %d", resp.StatusCode)
	}
	return nil
}
