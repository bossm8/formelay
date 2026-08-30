// Package notify defines the delivery-channel abstraction (Notifier) and a
// registry mapping a YAML `type:` string to a Go implementation. Built-in
// implementations live in the email, discord, and webhook subpackages; new
// channels are added the same way, with zero changes to core pipeline code.
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/bossm8/formelay/internal/render"
)

// RenderedMessage is the fully rendered, channel-specific payload ready to
// send. Field values have already been through template rendering (and, for
// HTML, escaping) by the time a Notifier sees them.
type RenderedMessage struct {
	Subject     string
	Body        []byte
	ContentType string
	Meta        map[string]string
}

// Notifier is implemented by every delivery channel.
type Notifier interface {
	Send(ctx context.Context, msg RenderedMessage) error
	Type() string
}

// TemplateRef names one template a Notifier needs rendered before Send, and
// under which RenderedMessage field the caller should place the result.
type TemplateRef struct {
	// Key is "subject" or "body" — the orchestration layer maps the
	// rendered result onto the corresponding RenderedMessage field.
	Key         string
	Kind        render.Kind
	Path        string
	Inline      string
	ContentType string
}

// TemplateProvider is optionally implemented by a Notifier so the
// orchestration layer can discover and render exactly the templates this
// channel needs, without hardcoding per-channel-type knowledge outside the
// channel's own package.
type TemplateProvider interface {
	TemplateRefs() []TemplateRef
}

// ReplyToFieldProvider is optionally implemented by a Notifier that wants
// Reply-To populated from a named submitted field (currently: email), the
// same optional-interface pattern as TemplateProvider — so the
// orchestration layer doesn't need per-channel-type knowledge to support it.
type ReplyToFieldProvider interface {
	ReplyToField() string
}

// NewNotifierFunc constructs a Notifier from a channel's raw YAML config
// (already decoded into map[string]any) plus global defaults (e.g. SMTP
// defaults) a channel may inherit from.
type NewNotifierFunc func(raw map[string]any, defaults GlobalDefaults) (Notifier, error)

// GlobalDefaults carries global config a channel implementation may inherit
// from when its own config omits a value (currently: SMTP defaults).
type GlobalDefaults struct {
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPasswordEnv string
	SMTPStartTLS    bool
	SMTPFrom        string
	SMTPTimeout     time.Duration
}

// Registry maps a channel `type:` string to a constructor.
type Registry struct {
	factories map[string]NewNotifierFunc
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]NewNotifierFunc{}}
}

func (r *Registry) Register(typeName string, fn NewNotifierFunc) {
	r.factories[typeName] = fn
}

func (r *Registry) Build(typeName string, raw map[string]any, defaults GlobalDefaults) (Notifier, error) {
	fn, ok := r.factories[typeName]
	if !ok {
		return nil, fmt.Errorf("notify: unknown channel type %q", typeName)
	}
	return fn(raw, defaults)
}
