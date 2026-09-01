//go:build integration

// Full-pipeline integration suite: a real HTTP submission is driven through
// handleSubmit exactly like submit_test.go's unit tests, but delivery goes
// to real receivers instead of an unreachable placeholder URL or an
// in-process httptest.Server — a real Mailpit SMTP server for the email
// channel, and the small custom cmd/webhookmirror recorder standing in for
// the discord channel (mechanically identical to the generic "webhook"
// channel type from the receiving end: both just POST JSON to a URL).
//
// The "webhook" channel type itself isn't exercised here because
// webhook.Config.Validate requires an https:// URL and the mirror has no
// TLS — internal/api/submit_test.go's TestSubmit_SpamFilterIncludeFields
// hits the same constraint and uses discord for the same reason.
//
// Run via `make test-integration` (drives everything through
// docker-compose, no local Mailpit/mirror needed), or manually:
//
//	docker compose -f docker-compose.test.yml up -d mailpit webhook-mirror
//	SMTP_TEST_HOST=127.0.0.1 SMTP_TEST_PORT=1025 SMTP_TEST_API=http://127.0.0.1:8025 \
//	  WEBHOOK_MIRROR_URL=http://127.0.0.1:8090 \
//	  go test -tags=integration ./internal/api/... -v
package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/captcha"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/notify/discord"
	"github.com/bossm8/formelay/internal/notify/email"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/spamfilter"
)

func smtpTestHost() string {
	if v := os.Getenv("SMTP_TEST_HOST"); v != "" {
		return v
	}
	return "127.0.0.1"
}

func smtpTestPort(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("SMTP_TEST_PORT"); v != "" {
		if _, err := strconv.Atoi(v); err != nil {
			t.Fatalf("invalid SMTP_TEST_PORT %q: %v", v, err)
		}
		return v
	}
	return "1025"
}

func mailpitAPI() string {
	if v := os.Getenv("SMTP_TEST_API"); v != "" {
		return v
	}
	return "http://127.0.0.1:8025"
}

func webhookMirrorURL() string {
	if v := os.Getenv("WEBHOOK_MIRROR_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8090"
}

func resetMailpit(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, mailpitAPI()+"/api/v1/messages", nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reset mailpit inbox: %v", err)
	}
	resp.Body.Close()
}

// mailpitAddress/mailpitMessage mirror Mailpit's JSON API shape — see
// internal/notify/email/integration_test.go for the full explanation of
// the (untagged, so verbatim-Go-field-name) wire format.
type mailpitAddress struct {
	Name    string
	Address string
}

type mailpitMessage struct {
	From    mailpitAddress
	To      []mailpitAddress
	ReplyTo []mailpitAddress
	Subject string
	Text    string
}

func latestMailpitMessage(t *testing.T) mailpitMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(mailpitAPI() + "/api/v1/message/latest")
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var msg mailpitMessage
			if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
				t.Fatalf("decode mailpit message: %v", err)
			}
			return msg
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no message appeared in mailpit within the timeout")
	return mailpitMessage{}
}

// mirrorRequest mirrors cmd/webhookmirror's capturedRequest JSON shape.
type mirrorRequest struct {
	Method     string      `json:"method"`
	Path       string      `json:"path"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
	ReceivedAt time.Time   `json:"received_at"`
}

func resetMirror(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, webhookMirrorURL()+"/_captured", nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reset webhook mirror: %v", err)
	}
	resp.Body.Close()
}

func capturedAtPath(t *testing.T, path string) []mirrorRequest {
	t.Helper()
	resp, err := http.Get(webhookMirrorURL() + "/_captured")
	if err != nil {
		t.Fatalf("fetch captured requests: %v", err)
	}
	defer resp.Body.Close()
	var all []mirrorRequest
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		t.Fatalf("decode captured requests: %v", err)
	}
	var matched []mirrorRequest
	for _, r := range all {
		if r.Path == path {
			matched = append(matched, r)
		}
	}
	return matched
}

// waitForCapture polls the mirror until at least one request has landed at
// path, since delivery happens in a goroutine (dispatchNormal/dispatchShared)
// concurrently with handleSubmit's response.
func waitForCapture(t *testing.T, path string) []mirrorRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := capturedAtPath(t, path); len(got) > 0 {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no request captured at %q within the timeout", path)
	return nil
}

// fakeTriggerClassifier reports spam whenever the submitted "message" field
// contains the literal substring "TRIGGER_SPAM" — a stand-in for a real AI
// provider, since this suite is about proving delivery content/routing is
// correct, not about classification itself (see plan: AI classifier stays
// fake, same technique TestSubmit_SpamFilterIncludeFields already uses).
type fakeTriggerClassifier struct{}

func (fakeTriggerClassifier) Classify(_ context.Context, data render.SubmissionData) (spamfilter.Verdict, error) {
	if strings.Contains(data.Fields["message"], "TRIGGER_SPAM") {
		return spamfilter.Verdict{IsSpam: true, Reason: "contains trigger phrase"}, nil
	}
	return spamfilter.Verdict{IsSpam: false}, nil
}

func newIntegrationServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	os.Setenv("INTEGRATION_DISCORD_TAGGED", webhookMirrorURL()+"/tagged/discord")
	os.Setenv("INTEGRATION_DISCORD_ROUTED_NORMAL", webhookMirrorURL()+"/routed/discord")
	os.Setenv("INTEGRATION_DISCORD_ROUTED_SPAMREVIEW", webhookMirrorURL()+"/routed/spam-review")
	t.Cleanup(func() {
		os.Unsetenv("INTEGRATION_DISCORD_TAGGED")
		os.Unsetenv("INTEGRATION_DISCORD_ROUTED_NORMAL")
		os.Unsetenv("INTEGRATION_DISCORD_ROUTED_SPAMREVIEW")
	})

	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
security:
  max_body_bytes: 65536
smtp_defaults:
  timeout: 5s
rate_limit:
  backend: memory
  default:
    per_ip: {rate: 1000, window: 1m, burst: 1000}
    per_form: {rate: 1000, window: 1m, burst: 1000}
    global: {rate: 10000, window: 1m, burst: 10000}
`)

	writeFile(t, filepath.Join(dir, "forms", "tagged.yaml"), `
id: tagged
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
fields:
  required: ["name", "email", "message"]
spam_filter:
  enabled: true
  provider:
    type: fake-trigger
  include_fields: ["message"]
  on_spam: deliver_tagged
  on_error: deliver
channels:
  - id: email-owner
    type: email
    config:
      to: ["owner@example.com"]
      from: "no-reply@example.com"
      host: "`+smtpTestHost()+`"
      port: `+smtpTestPort(t)+`
      body_type: text
      reply_to_field: "email"
      subject_template_inline: "New message from {{ .Fields.name }}"
      body_template_inline: "{{ .Fields.message }} | spam_suspected={{ .Meta.SpamSuspected }} | spam_reason={{ .Meta.SpamReason }}"
  - id: discord-alerts
    type: discord
    config:
      webhook_url_env: "INTEGRATION_DISCORD_TAGGED"
      template_inline: '{"name": {{ .Fields.name | json }}, "message": {{ .Fields.message | json }}, "spam_suspected": {{ .Meta.SpamSuspected }}, "spam_reason": {{ .Meta.SpamReason | json }}}'
`)

	writeFile(t, filepath.Join(dir, "forms", "routed.yaml"), `
id: routed
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
fields:
  required: ["name", "email", "message"]
spam_filter:
  enabled: true
  provider:
    type: fake-trigger
  include_fields: ["message"]
  on_spam: route
  on_error: deliver
  route:
    spam_channels: ["spam-review-discord"]
    spam_template: "spam-review.tmpl"
channels:
  - id: discord-alerts
    type: discord
    config:
      webhook_url_env: "INTEGRATION_DISCORD_ROUTED_NORMAL"
      template_inline: '{"name": {{ .Fields.name | json }}}'
  - id: spam-review-discord
    type: discord
    config:
      webhook_url_env: "INTEGRATION_DISCORD_ROUTED_SPAMREVIEW"
      template_inline: '{"unused": true}'
`)

	writeFile(t, filepath.Join(dir, "templates", "spam-review.tmpl"),
		`{"alert": "possible spam", "name": {{ .Fields.name | json }}, "message": {{ .Fields.message | json }}}`)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(email.Type, email.New)
	registries.Notify.Register(discord.Type, discord.New)
	registries.SpamFilter.Register("fake-trigger", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return fakeTriggerClassifier{}, nil
	})

	a := app.New(filepath.Join(dir, "config.yaml"), registries)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	return &Server{
		App:         a,
		RateLimiter: &fakeRateLimiter{},
		Audit:       audit.New(slog.New(slog.NewJSONHandler(io.Discard, nil))),
		Metrics:     metrics.New("test", "test", "test"),
		IDGen:       func() string { return "integration-test-request-id" },
	}
}

// TestIntegration_NormalDelivery proves a non-spam submission reaches every
// real receiver with the exact content submitted, including Reply-To
// sourced from the submitted "email" field.
func TestIntegration_NormalDelivery(t *testing.T) {
	resetMailpit(t)
	resetMirror(t)
	s := newIntegrationServer(t)

	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}, "message": {"Hello there"}}
	rec := doSubmit(t, s, "tagged", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	mail := latestMailpitMessage(t)
	if mail.Subject != "New message from Alice" {
		t.Errorf("Subject = %q", mail.Subject)
	}
	if len(mail.ReplyTo) != 1 || mail.ReplyTo[0].Address != "alice@example.com" {
		t.Errorf("ReplyTo = %v, want [alice@example.com]", mail.ReplyTo)
	}
	if got := mail.Text; got == "" || !strings.Contains(got, "Hello there") || !strings.Contains(got, "spam_suspected=false") {
		t.Errorf("body = %q, want it to contain the message and spam_suspected=false", got)
	}

	discordReqs := waitForCapture(t, "/tagged/discord")
	var payload struct {
		Name          string `json:"name"`
		Message       string `json:"message"`
		SpamSuspected bool   `json:"spam_suspected"`
	}
	if err := json.Unmarshal([]byte(discordReqs[len(discordReqs)-1].Body), &payload); err != nil {
		t.Fatalf("decode discord payload: %v", err)
	}
	if payload.Name != "Alice" || payload.Message != "Hello there" || payload.SpamSuspected {
		t.Errorf("discord payload = %+v", payload)
	}
}

// TestIntegration_DeliverTagged proves a spam-flagged submission with
// on_spam: deliver_tagged still reaches the normal channels, with the
// rendered content reflecting the spam verdict.
func TestIntegration_DeliverTagged(t *testing.T) {
	resetMailpit(t)
	resetMirror(t)
	s := newIntegrationServer(t)

	form := url.Values{"name": {"Bob"}, "email": {"bob@example.com"}, "message": {"buy now TRIGGER_SPAM"}}
	rec := doSubmit(t, s, "tagged", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	mail := latestMailpitMessage(t)
	if !strings.Contains(mail.Text, "spam_suspected=true") {
		t.Errorf("body = %q, want it to reflect spam_suspected=true", mail.Text)
	}

	discordReqs := waitForCapture(t, "/tagged/discord")
	var payload struct {
		SpamSuspected bool   `json:"spam_suspected"`
		SpamReason    string `json:"spam_reason"`
	}
	if err := json.Unmarshal([]byte(discordReqs[len(discordReqs)-1].Body), &payload); err != nil {
		t.Fatalf("decode discord payload: %v", err)
	}
	if !payload.SpamSuspected || payload.SpamReason == "" {
		t.Errorf("discord payload = %+v, want spam_suspected=true with a reason", payload)
	}
}

// TestIntegration_SpamRoute proves on_spam: route sends the shared
// spam-review payload only to route.spam_channels, and never touches the
// form's normal channels — the exact dispatchShared code path.
func TestIntegration_SpamRoute(t *testing.T) {
	resetMirror(t)
	s := newIntegrationServer(t)

	form := url.Values{"name": {"Eve"}, "email": {"eve@example.com"}, "message": {"cheap pills TRIGGER_SPAM"}}
	rec := doSubmit(t, s, "routed", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	spamReviewReqs := waitForCapture(t, "/routed/spam-review")
	var payload struct {
		Alert   string `json:"alert"`
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(spamReviewReqs[len(spamReviewReqs)-1].Body), &payload); err != nil {
		t.Fatalf("decode spam-review payload: %v", err)
	}
	if payload.Name != "Eve" || payload.Message != "cheap pills TRIGGER_SPAM" {
		t.Errorf("spam-review payload = %+v", payload)
	}

	if got := capturedAtPath(t, "/routed/discord"); len(got) != 0 {
		t.Errorf("expected the form's normal discord channel to receive nothing when routed, got %d request(s): %+v", len(got), got)
	}
}
