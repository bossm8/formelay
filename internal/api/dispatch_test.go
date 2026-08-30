package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/yamlutil"
)

// stubNotifier optionally implements notify.ReplyToFieldProvider, depending
// on replyToField — used to prove buildMessage only sets Meta["reply_to"]
// via that optional interface, never a hardcoded field name.
type stubNotifier struct{ replyToField string }

func (s *stubNotifier) Type() string                                       { return "stub" }
func (s *stubNotifier) Send(context.Context, notify.RenderedMessage) error { return nil }
func (s *stubNotifier) ReplyToField() string                               { return s.replyToField }

// stubNotifierNoReplyTo deliberately does not implement
// notify.ReplyToFieldProvider, unlike discord/webhook in production.
type stubNotifierNoReplyTo struct{}

func (s *stubNotifierNoReplyTo) Type() string                                       { return "stub" }
func (s *stubNotifierNoReplyTo) Send(context.Context, notify.RenderedMessage) error { return nil }

func TestBuildMessageReplyTo(t *testing.T) {
	t.Run("configured field present in submission sets reply_to", func(t *testing.T) {
		cc := &app.CompiledChannel{Notifier: &stubNotifier{replyToField: "message"}}
		data := render.SubmissionData{Fields: map[string]string{"message": "alice@example.com"}}
		msg := buildMessage(cc, data)
		if got := msg.Meta["reply_to"]; got != "alice@example.com" {
			t.Fatalf("Meta[reply_to] = %q, want %q", got, "alice@example.com")
		}
	})

	t.Run("configured field absent from submission leaves reply_to unset", func(t *testing.T) {
		cc := &app.CompiledChannel{Notifier: &stubNotifier{replyToField: "message"}}
		data := render.SubmissionData{Fields: map[string]string{"name": "Alice"}}
		msg := buildMessage(cc, data)
		if _, ok := msg.Meta["reply_to"]; ok {
			t.Fatalf("expected no reply_to, got %q", msg.Meta["reply_to"])
		}
	})

	t.Run("notifier without ReplyToFieldProvider leaves reply_to unset", func(t *testing.T) {
		cc := &app.CompiledChannel{Notifier: &stubNotifierNoReplyTo{}}
		data := render.SubmissionData{Fields: map[string]string{"message": "alice@example.com"}}
		msg := buildMessage(cc, data)
		if _, ok := msg.Meta["reply_to"]; ok {
			t.Fatalf("expected no reply_to, got %q", msg.Meta["reply_to"])
		}
	})
}

// TestBuildMessageTemplateErrorStopsAtFirstFailureDeterministically is a
// regression test: buildMessage used to range over cc.Templates (a map),
// so on a channel with more than one failing/succeeding template key, the
// resulting msg.Body / Meta["render_error"] combination depended on Go's
// randomized map iteration order. A template key that renders successfully
// *after* one that failed would silently overwrite msg.Body with real
// content while a stale Meta["render_error"] from the earlier failure was
// left in place. buildMessage now iterates keys in sorted order and stops
// at the first failure, so Body is never populated once a render error has
// been recorded.
func TestBuildMessageTemplateErrorStopsAtFirstFailureDeterministically(t *testing.T) {
	// "aaa_bad" sorts before "zzz_good": referencing a struct field that
	// doesn't exist on render.SubmissionData is a template *execution*
	// error (not a parse error), so this fails only when Execute runs.
	bad, err := render.Parse(render.KindText, "bad", "{{.NoSuchField}}")
	if err != nil {
		t.Fatalf("parse bad template: %v", err)
	}
	good, err := render.Parse(render.KindText, "good", "ok")
	if err != nil {
		t.Fatalf("parse good template: %v", err)
	}

	cc := &app.CompiledChannel{
		Notifier: &stubNotifierNoReplyTo{},
		Templates: map[string]*render.Template{
			"aaa_bad":  bad,
			"zzz_good": good,
		},
	}
	msg := buildMessage(cc, render.SubmissionData{})

	if _, ok := msg.Meta["render_error"]; !ok {
		t.Fatal("expected Meta[render_error] to be set")
	}
	if !strings.Contains(msg.Meta["render_error"], "NoSuchField") {
		t.Fatalf("render_error = %q, want it to reference the failing (sorted-first) template", msg.Meta["render_error"])
	}
	if msg.Body != nil {
		t.Fatalf("Body = %q, want nil: a later-processed successful template must not overwrite Body once an earlier one failed", msg.Body)
	}
}

// TestDispatchSharedMissingChannelRecordsFailure is a regression test:
// dispatchShared used to silently `continue` past a channel id in
// channelIDs that no longer exists in cf.Channels (e.g. a stale/typo'd
// spam_filter.route.spam_channels entry, or a channel disabled since the
// form was last validated), recording no ChannelResult for it at all. That
// silently narrowed what channels_required: all evaluated over. It must
// now show up as an explicit failed result.
func TestDispatchSharedMissingChannelRecordsFailure(t *testing.T) {
	s := &Server{}
	tmpl, err := render.Parse(render.KindText, "shared", `{"ok":true}`)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	cf := &app.CompiledForm{
		Config:   &config.FormConfig{DisplayName: "Contact"},
		Channels: map[string]*app.CompiledChannel{}, // "ghost" intentionally absent
	}

	results := s.dispatchShared(context.Background(), "contact", cf, []string{"ghost"}, tmpl, render.SubmissionData{})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].ID != "ghost" || results[0].Success {
		t.Fatalf("expected a failed result for missing channel %q, got %+v", "ghost", results[0])
	}
	if allSuccess(results) {
		t.Fatalf("channels_required: all must see the missing channel as a failure, not silently pass")
	}
}

// TestAnySuccessAllSuccessOnEmptyResults documents current (intentional,
// not a bug) behavior: a form dispatching to zero channels — either because
// it has none configured, or because dispatchShared/dispatchNormal produced
// no results — is treated as vacuously successful by both aggregators. This
// pins the behavior down so a future change to it is deliberate.
func TestAnySuccessAllSuccessOnEmptyResults(t *testing.T) {
	var empty []audit.ChannelResult
	if !anySuccess(empty) {
		t.Fatal("anySuccess(nil) should be vacuously true")
	}
	if !allSuccess(empty) {
		t.Fatal("allSuccess(nil) should be vacuously true")
	}
}

// sequencedRateLimiter returns a scripted sequence of allow/deny results,
// one per call, repeating the last entry once exhausted. panicIfCalled
// lets a test prove a code path never touches the limiter at all.
type sequencedRateLimiter struct {
	mu            sync.Mutex
	results       []bool
	err           error
	panicIfCalled bool
	calls         int
}

func (f *sequencedRateLimiter) Allow(context.Context, string, float64, float64, time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicIfCalled {
		panic("Allow called but should not have been reached")
	}
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	if len(f.results) == 0 {
		return true, nil
	}
	r := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return r, nil
}

func rateLimitedChannel(t *testing.T) *config.ChannelRateLimitConfig {
	t.Helper()
	return &config.ChannelRateLimitConfig{Rate: 1, Window: yamlutil.Duration(time.Minute), Burst: 1}
}

func TestAwaitOutboundTokenOnLimitFail(t *testing.T) {
	rl := rateLimitedChannel(t)
	rl.OnLimit = "fail"
	fake := &sequencedRateLimiter{results: []bool{false}}
	s := &Server{RateLimiter: fake}

	waited, allowed, err := s.awaitOutboundToken(context.Background(), "contact", "email-owner", rl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected not allowed")
	}
	if waited != 0 {
		t.Fatalf("on_limit: fail must never wait, got %v", waited)
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly 1 Allow call for on_limit: fail, got %d", fake.calls)
	}
}

func TestAwaitOutboundTokenWaitEventuallyAllowed(t *testing.T) {
	orig := outboundPollInterval
	outboundPollInterval = time.Millisecond
	t.Cleanup(func() { outboundPollInterval = orig })

	rl := rateLimitedChannel(t)
	rl.OnLimit = "wait"
	rl.MaxWait = yamlutil.Duration(time.Second)
	fake := &sequencedRateLimiter{results: []bool{false, false, true}}
	s := &Server{RateLimiter: fake}

	waited, allowed, err := s.awaitOutboundToken(context.Background(), "contact", "email-owner", rl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected eventually allowed")
	}
	if waited <= 0 {
		t.Fatalf("expected non-zero wait once polling happened, got %v", waited)
	}
	if fake.calls != 3 {
		t.Fatalf("expected 3 Allow calls (1 immediate + 2 polls), got %d", fake.calls)
	}
}

func TestAwaitOutboundTokenWaitTimesOut(t *testing.T) {
	orig := outboundPollInterval
	outboundPollInterval = time.Millisecond
	t.Cleanup(func() { outboundPollInterval = orig })

	rl := rateLimitedChannel(t)
	rl.OnLimit = "wait"
	rl.MaxWait = yamlutil.Duration(5 * time.Millisecond)
	fake := &sequencedRateLimiter{results: []bool{false}} // always denies
	s := &Server{RateLimiter: fake}

	waited, allowed, err := s.awaitOutboundToken(context.Background(), "contact", "email-owner", rl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected not allowed once max_wait elapses")
	}
	if waited < rl.MaxWait.Std() {
		t.Fatalf("expected waited >= max_wait (%v), got %v", rl.MaxWait.Std(), waited)
	}
}

func TestAwaitOutboundTokenBackendErrorFailsOpen(t *testing.T) {
	rl := rateLimitedChannel(t)
	fake := &sequencedRateLimiter{err: errors.New("backend unreachable")}
	s := &Server{RateLimiter: fake}

	_, _, err := s.awaitOutboundToken(context.Background(), "contact", "email-owner", rl)
	if err == nil {
		t.Fatal("expected the backend error to be returned, not swallowed")
	}
	// sendOne's contract (not exercised directly here): err != nil means
	// "don't treat as rate_limited" — the caller falls through to attempt
	// the send anyway, matching handleSubmit's existing inbound convention.
}

func TestSendOneSkipsRateLimitEntirelyWhenUnset(t *testing.T) {
	s := &Server{
		RateLimiter: &sequencedRateLimiter{panicIfCalled: true},
		Metrics:     metrics.New("test", "test", "test"),
	}
	cc := &app.CompiledChannel{Notifier: &stubNotifierNoReplyTo{}} // RateLimit left nil
	result := s.sendOne(context.Background(), "contact", "email-owner", cc, notify.RenderedMessage{})
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
}

func TestSendOneRecordsRateLimited(t *testing.T) {
	rl := rateLimitedChannel(t)
	rl.OnLimit = "fail"
	s := &Server{
		RateLimiter: &sequencedRateLimiter{results: []bool{false}},
		Metrics:     metrics.New("test", "test", "test"),
	}
	cc := &app.CompiledChannel{Notifier: &stubNotifierNoReplyTo{}, RateLimit: rl}
	result := s.sendOne(context.Background(), "contact", "email-owner", cc, notify.RenderedMessage{})
	if result.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(result.Error, "rate_limited") {
		t.Fatalf("expected a rate_limited error, got %q", result.Error)
	}
}

func TestBuildMessageDoesNotWriteSpamMeta(t *testing.T) {
	cc := &app.CompiledChannel{Notifier: &stubNotifierNoReplyTo{}}
	data := render.SubmissionData{
		Meta: render.RequestMeta{SpamSuspected: true, SpamReason: "looked like spam"},
	}
	msg := buildMessage(cc, data)
	if _, ok := msg.Meta["spam_suspected"]; ok {
		t.Fatalf("spam_suspected should never be written to RenderedMessage.Meta (templates read SubmissionData.Meta directly), got %q", msg.Meta["spam_suspected"])
	}
	if _, ok := msg.Meta["spam_reason"]; ok {
		t.Fatalf("spam_reason should never be written to RenderedMessage.Meta (templates read SubmissionData.Meta directly), got %q", msg.Meta["spam_reason"])
	}
}
