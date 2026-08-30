// Package app is the composition root: it owns the single atomically
// swappable Runtime (raw config + everything built from it — notifiers,
// captcha verifiers, spam classifiers, parsed templates), so a reader can
// never observe raw config and compiled runtime objects that disagree with
// each other, even momentarily.
package app

import (
	"fmt"
	"sync/atomic"

	"github.com/bossm8/formelay/internal/captcha"
	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/spamfilter"
	"github.com/bossm8/formelay/internal/yamlutil"
)

// CompiledChannel pairs a live Notifier with its parsed templates, keyed by
// notify.TemplateRef.Key ("subject", "body").
type CompiledChannel struct {
	ID          string
	Notifier    notify.Notifier
	Templates   map[string]*render.Template
	ContentType map[string]string
	// RateLimit, if set, throttles outbound deliveries on this channel —
	// see config.ChannelRateLimitConfig. A passthrough, no compilation
	// needed (unlike Templates).
	RateLimit *config.ChannelRateLimitConfig
}

// CompiledForm is everything built from one FormConfig.
type CompiledForm struct {
	Config             *config.FormConfig
	Channels           map[string]*CompiledChannel // by channel id
	CaptchaVerifier    captcha.Verifier
	SpamClassifier     spamfilter.Classifier
	SpamRouteTemplates map[string]*render.Template // "spam" | "error" -> template
}

// Runtime is one atomically-published, internally-consistent config +
// compiled-runtime snapshot.
type Runtime struct {
	Config *config.Snapshot
	Forms  map[string]*CompiledForm
}

// Registries holds the registered channel/captcha/spam-filter implementations.
type Registries struct {
	Notify     *notify.Registry
	Captcha    *captcha.Registry
	SpamFilter *spamfilter.Registry
}

type App struct {
	globalPath string
	registries Registries
	ptr        atomic.Pointer[Runtime]
}

func New(globalPath string, registries Registries) *App {
	return &App{globalPath: globalPath, registries: registries}
}

func (a *App) Current() *Runtime {
	return a.ptr.Load()
}

// Reload loads config.yaml + forms_dir, builds every runtime object it
// implies, and only then publishes the whole thing atomically. On any
// failure (bad YAML, unknown channel/captcha/classifier type, a template
// that fails to parse, ...) the previous Runtime keeps serving unchanged.
func (a *App) Reload() error {
	snap, err := config.Load(a.globalPath)
	if err != nil {
		return err
	}

	forms := map[string]*CompiledForm{}
	for id, fc := range snap.Forms {
		cf, err := a.compileForm(snap.Global, fc)
		if err != nil {
			return fmt.Errorf("compile form %q: %w", id, err)
		}
		forms[id] = cf
	}

	a.ptr.Store(&Runtime{Config: snap, Forms: forms})
	return nil
}

func (a *App) compileForm(global *config.GlobalConfig, fc *config.FormConfig) (*CompiledForm, error) {
	cf := &CompiledForm{Config: fc, Channels: map[string]*CompiledChannel{}}

	defaults := notify.GlobalDefaults{
		SMTPHost:        global.SMTPDefaults.Host,
		SMTPPort:        global.SMTPDefaults.Port,
		SMTPUsername:    global.SMTPDefaults.Username,
		SMTPPasswordEnv: global.SMTPDefaults.PasswordEnv,
		SMTPStartTLS:    global.SMTPDefaults.StartTLS,
		SMTPFrom:        global.SMTPDefaults.From,
		SMTPTimeout:     global.SMTPDefaults.Timeout.Std(),
	}

	for _, ch := range fc.Channels {
		if !ch.IsEnabled() {
			continue
		}
		n, err := a.registries.Notify.Build(ch.Type, ch.Config, defaults)
		if err != nil {
			return nil, fmt.Errorf("channel %q: %w", ch.ID, err)
		}
		cc := &CompiledChannel{ID: ch.ID, Notifier: n, Templates: map[string]*render.Template{}, ContentType: map[string]string{}, RateLimit: ch.RateLimit}
		if tp, ok := n.(notify.TemplateProvider); ok {
			for _, ref := range tp.TemplateRefs() {
				source, err := render.ResolveSource(global.TemplatesDir, ref.Path, ref.Inline)
				if err != nil {
					return nil, fmt.Errorf("channel %q template %q: %w", ch.ID, ref.Key, err)
				}
				tmpl, err := render.Parse(ref.Kind, ch.ID+"."+ref.Key, source)
				if err != nil {
					return nil, fmt.Errorf("channel %q template %q: %w", ch.ID, ref.Key, err)
				}
				cc.Templates[ref.Key] = tmpl
				cc.ContentType[ref.Key] = ref.ContentType
			}
		}
		cf.Channels[ch.ID] = cc
	}

	if fc.Captcha.Enabled {
		raw, err := yamlutil.ToMap(fc.Captcha)
		if err != nil {
			return nil, fmt.Errorf("captcha: %w", err)
		}
		v, err := a.registries.Captcha.Build(fc.Captcha.Provider, raw)
		if err != nil {
			return nil, fmt.Errorf("captcha: %w", err)
		}
		cf.CaptchaVerifier = v
	}

	if fc.SpamFilter.Enabled {
		raw, err := yamlutil.ToMap(fc.SpamFilter.Provider)
		if err != nil {
			return nil, fmt.Errorf("spam_filter: %w", err)
		}

		var prompts spamfilter.PromptSource
		if fc.SpamFilter.SystemTemplate != "" || fc.SpamFilter.SystemInline != "" {
			s, err := render.ResolveSource(global.TemplatesDir, fc.SpamFilter.SystemTemplate, fc.SpamFilter.SystemInline)
			if err != nil {
				return nil, fmt.Errorf("spam_filter: system_template: %w", err)
			}
			prompts.SystemSource = s
		}
		if fc.SpamFilter.UserTemplate != "" || fc.SpamFilter.UserInline != "" {
			s, err := render.ResolveSource(global.TemplatesDir, fc.SpamFilter.UserTemplate, fc.SpamFilter.UserInline)
			if err != nil {
				return nil, fmt.Errorf("spam_filter: user_template: %w", err)
			}
			prompts.UserSource = s
		}

		classifier, err := a.registries.SpamFilter.Build(fc.SpamFilter.Provider.Type, raw, prompts)
		if err != nil {
			return nil, fmt.Errorf("spam_filter: %w", err)
		}
		cf.SpamClassifier = classifier

		cf.SpamRouteTemplates = map[string]*render.Template{}
		if fc.SpamFilter.Route.SpamTemplate != "" {
			src, err := render.ResolveSource(global.TemplatesDir, fc.SpamFilter.Route.SpamTemplate, "")
			if err != nil {
				return nil, fmt.Errorf("spam_filter: route.spam_template: %w", err)
			}
			tmpl, err := render.Parse(render.KindText, "spam-review", src)
			if err != nil {
				return nil, fmt.Errorf("spam_filter: route.spam_template: %w", err)
			}
			cf.SpamRouteTemplates["spam"] = tmpl
		}
		errTemplate := fc.SpamFilter.Route.ErrorTemplate
		if errTemplate == "" {
			errTemplate = fc.SpamFilter.Route.SpamTemplate
		}
		if errTemplate != "" {
			src, err := render.ResolveSource(global.TemplatesDir, errTemplate, "")
			if err != nil {
				return nil, fmt.Errorf("spam_filter: route.error_template: %w", err)
			}
			tmpl, err := render.Parse(render.KindText, "spam-review-error", src)
			if err != nil {
				return nil, fmt.Errorf("spam_filter: route.error_template: %w", err)
			}
			cf.SpamRouteTemplates["error"] = tmpl
		}
	}

	return cf, nil
}
