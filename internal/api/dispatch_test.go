package api

import (
	"context"
	"testing"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
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
