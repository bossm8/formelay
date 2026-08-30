package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bossm8/formelay/internal/captcha"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/spamfilter"
)

type fakeNotifier struct{}

func (fakeNotifier) Send(context.Context, notify.RenderedMessage) error { return nil }
func (fakeNotifier) Type() string                                       { return "fake" }

// fakeTemplatedNotifier additionally implements notify.TemplateProvider, so
// tests can exercise template-resolution failures during compileForm.
type fakeTemplatedNotifier struct {
	fakeNotifier
	path, inline string
}

func (f fakeTemplatedNotifier) TemplateRefs() []notify.TemplateRef {
	return []notify.TemplateRef{{Key: "body", Kind: render.KindText, Path: f.path, Inline: f.inline, ContentType: "text/plain"}}
}

type fakeClassifier struct{}

func (fakeClassifier) Classify(context.Context, render.SubmissionData) (spamfilter.Verdict, error) {
	return spamfilter.Verdict{}, nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGlobalConfig(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
`)
}

func newRegistries() Registries {
	return Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
}

func TestAppCurrentNilBeforeReload(t *testing.T) {
	a := New("/nonexistent/config.yaml", newRegistries())
	if a.Current() != nil {
		t.Fatal("expected Current() to be nil before any Reload()")
	}
}

func TestAppReloadHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: fake
`)
	regs := newRegistries()
	regs.Notify.Register("fake", func(map[string]any, notify.GlobalDefaults) (notify.Notifier, error) {
		return fakeNotifier{}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	rt := a.Current()
	if rt == nil {
		t.Fatal("expected Current() to be populated after Reload()")
	}
	cf, ok := rt.Forms["contact"]
	if !ok {
		t.Fatal("expected form 'contact' to be compiled")
	}
	if _, ok := cf.Channels["ch1"]; !ok {
		t.Fatal("expected channel 'ch1' to be compiled")
	}
}

// TestAppReloadAllOrNothing locks in the documented guarantee: a single bad
// form aborts the whole reload, and the previously published Runtime keeps
// serving unchanged.
func TestAppReloadAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: fake
`)
	regs := newRegistries()
	regs.Notify.Register("fake", func(map[string]any, notify.GlobalDefaults) (notify.Notifier, error) {
		return fakeNotifier{}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	if err := a.Reload(); err != nil {
		t.Fatalf("first reload: %v", err)
	}
	first := a.Current()

	writeFile(t, filepath.Join(dir, "forms.d", "broken.yaml"), `
id: broken
auth:
  site_key: "key"
channels:
  - id: chx
    type: does-not-exist
`)
	if err := a.Reload(); err == nil {
		t.Fatal("expected second reload to fail due to unknown channel type")
	}
	if a.Current() != first {
		t.Fatal("expected Current() to be unchanged after a failed Reload()")
	}
}

// TestCompileFormSkipsDisabledChannel proves a disabled channel's config is
// never built or validated at all: an unregistered/bogus type on a disabled
// channel must not fail reload.
func TestCompileFormSkipsDisabledChannel(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: does-not-exist
    enabled: false
`)
	a := New(filepath.Join(dir, "config.yaml"), newRegistries())
	if err := a.Reload(); err != nil {
		t.Fatalf("expected disabled channel with an unregistered type to be skipped, got error: %v", err)
	}
	cf := a.Current().Forms["contact"]
	if _, ok := cf.Channels["ch1"]; ok {
		t.Fatal("disabled channel must not be compiled")
	}
}

func TestCompileFormUnknownChannelType(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: does-not-exist
`)
	a := New(filepath.Join(dir, "config.yaml"), newRegistries())
	if err := a.Reload(); err == nil {
		t.Fatal("expected reload to fail for unknown channel type")
	}
}

// TestCompileFormUnknownCaptchaProviderOnlyWhenEnabled documents the
// two-layer validation design: config.ValidateForm doesn't check the
// captcha provider name against the known set, only app.Reload's registry
// lookup does — and only when captcha is actually enabled.
func TestCompileFormUnknownCaptchaProviderOnlyWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	formPath := filepath.Join(dir, "forms.d", "contact.yaml")
	writeFile(t, formPath, `
id: contact
auth:
  site_key: "key"
captcha:
  enabled: false
  provider: does-not-exist
  secret_env: SECRET
  response_field: resp
`)
	a := New(filepath.Join(dir, "config.yaml"), newRegistries())
	if err := a.Reload(); err != nil {
		t.Fatalf("disabled captcha with a bogus provider must not fail reload: %v", err)
	}

	writeFile(t, formPath, `
id: contact
auth:
  site_key: "key"
captcha:
  enabled: true
  provider: does-not-exist
  secret_env: SECRET
  response_field: resp
`)
	if err := a.Reload(); err == nil {
		t.Fatal("expected reload to fail for unknown captcha provider once enabled")
	}
}

// TestCompileFormSpamRouteTemplateFallback locks in the fallback rule:
// route.error_template defaults to route.spam_template when unset.
func TestCompileFormSpamRouteTemplateFallback(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "templates", "spam-review.tmpl"), `{"spam": true}`)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
spam_filter:
  enabled: true
  provider:
    type: fake
  route:
    spam_template: "spam-review.tmpl"
`)
	regs := newRegistries()
	regs.SpamFilter.Register("fake", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return fakeClassifier{}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	cf := a.Current().Forms["contact"]
	if _, ok := cf.SpamRouteTemplates["spam"]; !ok {
		t.Fatal(`expected SpamRouteTemplates["spam"] to be set from route.spam_template`)
	}
	if _, ok := cf.SpamRouteTemplates["error"]; !ok {
		t.Fatal(`expected SpamRouteTemplates["error"] to fall back to route.spam_template when error_template is unset`)
	}
}

// TestCompileFormSpamRouteTemplatesEmptyWhenNeitherSet documents that with
// neither template configured (and on_spam/on_error left at the non-route
// default), no SpamRouteTemplates entries are built and no error occurs.
func TestCompileFormSpamRouteTemplatesEmptyWhenNeitherSet(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
spam_filter:
  enabled: true
  provider:
    type: fake
`)
	regs := newRegistries()
	regs.SpamFilter.Register("fake", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return fakeClassifier{}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	cf := a.Current().Forms["contact"]
	if len(cf.SpamRouteTemplates) != 0 {
		t.Fatalf("expected no SpamRouteTemplates entries when neither template is set, got %v", cf.SpamRouteTemplates)
	}
}

// TestReloadFailsWhenSpamRouteMissingTemplate is the app-level half of the
// regression test for the config validation fix: on_spam: route with no
// spam_template must fail Reload (via config.Load -> ValidateForm), not
// silently compile and drop delivery at runtime.
func TestReloadFailsWhenSpamRouteMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: fake
spam_filter:
  enabled: true
  provider:
    type: fake
  on_spam: route
  route:
    spam_channels: ["ch1"]
`)
	regs := newRegistries()
	regs.Notify.Register("fake", func(map[string]any, notify.GlobalDefaults) (notify.Notifier, error) {
		return fakeNotifier{}, nil
	})
	regs.SpamFilter.Register("fake", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return fakeClassifier{}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	if err := a.Reload(); err == nil {
		t.Fatal("expected reload to fail: on_spam=route with no spam_template must be rejected by config validation")
	}
}

// TestCompileFormTemplateResolutionFailureFailsReload proves a channel
// template that can't be resolved (nonexistent file) fails Reload with an
// error that names the offending channel.
func TestCompileFormTemplateResolutionFailureFailsReload(t *testing.T) {
	dir := t.TempDir()
	writeGlobalConfig(t, dir)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: "key"
channels:
  - id: ch1
    type: fake
`)
	regs := newRegistries()
	regs.Notify.Register("fake", func(map[string]any, notify.GlobalDefaults) (notify.Notifier, error) {
		return fakeTemplatedNotifier{path: "does-not-exist.tmpl"}, nil
	})
	a := New(filepath.Join(dir, "config.yaml"), regs)
	err := a.Reload()
	if err == nil {
		t.Fatal("expected reload to fail when a channel's template file doesn't exist")
	}
	if !strings.Contains(err.Error(), "ch1") {
		t.Fatalf("expected error to reference the failing channel id %q, got: %v", "ch1", err)
	}
}
