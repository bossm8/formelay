// Package email implements the Notifier for SMTP delivery.
package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"

	gomail "github.com/wneessen/go-mail"

	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/yamlutil"
)

const Type = "email"

type Config struct {
	To              []string          `yaml:"to"`
	Host            string            `yaml:"host"`
	Port            int               `yaml:"port"`
	Username        string            `yaml:"username"`
	PasswordEnv     string            `yaml:"password_env"`
	StartTLS        bool              `yaml:"starttls"`
	From            string            `yaml:"from"`
	Timeout         yamlutil.Duration `yaml:"timeout"`
	SubjectTemplate string            `yaml:"subject_template"`
	SubjectInline   string            `yaml:"subject_template_inline"`
	BodyTemplate    string            `yaml:"body_template"`
	BodyInline      string            `yaml:"body_template_inline"`
	BodyType        string            `yaml:"body_type"` // html | text
	// ReplyToField, if set, names a submitted form field whose value is used
	// as the Reply-To header. The value is validated with net/mail.ParseAddress
	// (not merely interpolated) — this is what actually prevents SMTP header
	// injection via a crafted field value; see plan Security Model.
	ReplyToField string `yaml:"reply_to_field"`
}

func (c *Config) Validate() error {
	if len(c.To) == 0 {
		return errors.New("email: 'to' must have at least one address")
	}
	if c.Host == "" {
		return errors.New("email: 'host' required (or global smtp_defaults)")
	}
	if c.From == "" {
		return errors.New("email: 'from' required (or global smtp_defaults)")
	}
	if c.BodyType != "html" && c.BodyType != "text" {
		return errors.New("email: body_type must be 'html' or 'text'")
	}
	if c.BodyTemplate == "" && c.BodyInline == "" {
		return errors.New("email: 'body_template' or 'body_template_inline' required")
	}
	if c.PasswordEnv != "" && os.Getenv(c.PasswordEnv) == "" {
		return fmt.Errorf("email: environment variable %q referenced by 'password_env' is not set", c.PasswordEnv)
	}
	return nil
}

type emailNotifier struct {
	cfg Config
}

// New is a notify.NewNotifierFunc. Values not set in the channel's own
// config inherit from the global smtp_defaults block.
func New(raw map[string]any, defaults notify.GlobalDefaults) (notify.Notifier, error) {
	cfg := Config{
		Host:        defaults.SMTPHost,
		Port:        defaults.SMTPPort,
		Username:    defaults.SMTPUsername,
		PasswordEnv: defaults.SMTPPasswordEnv,
		StartTLS:    defaults.SMTPStartTLS,
		From:        defaults.SMTPFrom,
		Timeout:     yamlutil.Duration(defaults.SMTPTimeout),
		BodyType:    "html",
	}
	if err := yamlutil.Decode(raw, &cfg); err != nil {
		return nil, fmt.Errorf("email: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &emailNotifier{cfg: cfg}, nil
}

func (e *emailNotifier) Type() string { return Type }

func (e *emailNotifier) TemplateRefs() []notify.TemplateRef {
	bodyKind := render.KindText
	bodyContentType := "text/plain"
	if e.cfg.BodyType == "html" {
		bodyKind = render.KindHTML
		bodyContentType = "text/html"
	}
	return []notify.TemplateRef{
		{Key: "subject", Kind: render.KindText, Path: e.cfg.SubjectTemplate, Inline: e.cfg.SubjectInline, ContentType: "text/plain"},
		{Key: "body", Kind: bodyKind, Path: e.cfg.BodyTemplate, Inline: e.cfg.BodyInline, ContentType: bodyContentType},
	}
}

func (e *emailNotifier) Send(ctx context.Context, msg notify.RenderedMessage) error {
	m := gomail.NewMsg()
	if err := m.From(e.cfg.From); err != nil {
		return fmt.Errorf("email: invalid From address: %w", err)
	}
	if err := m.To(e.cfg.To...); err != nil {
		return fmt.Errorf("email: invalid To address: %w", err)
	}
	if replyTo, ok := msg.Meta["reply_to"]; ok && replyTo != "" {
		if _, err := mail.ParseAddress(replyTo); err != nil {
			return fmt.Errorf("email: rejecting reply-to header from submitted content: %w", err)
		}
		if err := m.ReplyTo(replyTo); err != nil {
			return fmt.Errorf("email: setting reply-to: %w", err)
		}
	}
	m.Subject(msg.Subject)
	if e.cfg.BodyType == "html" {
		m.SetBodyString(gomail.TypeTextHTML, string(msg.Body))
	} else {
		m.SetBodyString(gomail.TypeTextPlain, string(msg.Body))
	}

	opts := []gomail.Option{gomail.WithPort(e.cfg.Port), gomail.WithTimeout(e.cfg.Timeout.Std())}
	if e.cfg.StartTLS {
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	} else {
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	if e.cfg.Username != "" {
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(e.cfg.Username), gomail.WithPassword(os.Getenv(e.cfg.PasswordEnv)))
	}
	client, err := gomail.NewClient(e.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("email: build SMTP client: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}
