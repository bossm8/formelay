package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/captcha"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/notify/discord"
	"github.com/bossm8/formelay/internal/notify/webhook"
	"github.com/bossm8/formelay/internal/ratelimit"
	"github.com/bossm8/formelay/internal/render"
	"github.com/bossm8/formelay/internal/spamfilter"
)

// fakeRateLimiter always allows, so tests exercise the rest of the pipeline.
type fakeRateLimiter struct{ denyAll bool }

func (f *fakeRateLimiter) Allow(_ context.Context, _ string, _, _ float64, _ time.Duration) (bool, error) {
	return !f.denyAll, nil
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

func newTestServer(t *testing.T, formsYAML string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("TEST_WEBHOOK_TOKEN", "wh-secret")
	t.Cleanup(func() { os.Unsetenv("TEST_WEBHOOK_TOKEN") })

	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
security:
  max_body_bytes: 65536
rate_limit:
  backend: memory
  default:
    per_ip: {rate: 100, window: 1m, burst: 100}
    per_form: {rate: 100, window: 1m, burst: 100}
    global: {rate: 1000, window: 1m, burst: 1000}
`)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), formsYAML)
	writeFile(t, filepath.Join(dir, "templates", "body.tmpl"), `{"name": {{ .Fields.name | json }}}`)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(webhook.Type, webhook.New)

	a := app.New(filepath.Join(dir, "config.yaml"), registries)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	var bgWG sync.WaitGroup
	return &Server{
		App:           a,
		RateLimiter:   &fakeRateLimiter{},
		Audit:         audit.New(log),
		Metrics:       metrics.New("test", "test", "test"),
		IDGen:         func() string { return "test-request-id" },
		BackgroundCtx: context.Background(),
		BackgroundWG:  &bgWG,
	}, dir
}

func doSubmit(t *testing.T, s *Server, formID string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/f/"+formID+"/submit", strings.NewReader(form.Encode()))
	req.SetPathValue("formID", formID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://example.com")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handleSubmit(rec, req)
	return rec
}

const baseForm = `
id: contact
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
honeypot:
  field_name: "website"
fields:
  required: ["name", "email"]
  validators:
    email: "email"
channels:
  - id: wh
    type: webhook
    config:
      url: "https://example.invalid/hook"
      template: "body.tmpl"
`

func TestSubmit_Success(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	// Delivery will fail (unreachable URL) but the endpoint should still
	// respond based on channels_required (default "any" -> 502 since it's
	// the only channel and it fails). We assert the pipeline reached
	// dispatch, not a specific HTTP status tied to network availability.
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmit_OriginDenied(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	req := httptest.NewRequest(http.MethodPost, "/f/contact/submit", strings.NewReader(""))
	req.SetPathValue("formID", "contact")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.handleSubmit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmit_UnknownForm(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	rec := doSubmit(t, s, "nope", url.Values{}, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSubmit_InvalidSiteKey(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmit_ValidationFailed(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	form := url.Values{"name": {"Alice"}} // missing required email
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmit_HoneypotFakeSuccess(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	form := url.Values{"name": {"Bot"}, "email": {"bot@example.com"}, "website": {"http://spam.example"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (fake success), got %d: %s", rec.Code, rec.Body.String())
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatalf("expected success=true for honeypot fake-success response")
	}
}

func TestSubmit_RateLimited(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	s.RateLimiter = &fakeRateLimiter{denyAll: true}
	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

// fakeClassifierRecorder captures the SubmissionData a fakeClassifier was
// called with, so a test can assert exactly what the "AI provider" saw.
type fakeClassifierRecorder struct {
	mu       sync.Mutex
	called   bool
	received render.SubmissionData
}

func (r *fakeClassifierRecorder) recordAndPass(data render.SubmissionData) spamfilter.Verdict {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called = true
	r.received = data
	return spamfilter.Verdict{IsSpam: false}
}

type fakeClassifier struct{ rec *fakeClassifierRecorder }

func (f *fakeClassifier) Classify(_ context.Context, data render.SubmissionData) (spamfilter.Verdict, error) {
	return f.rec.recordAndPass(data), nil
}

// TestSubmit_SpamFilterIncludeFields proves spam_filter.include_fields is
// scoped to exactly the AI classifier call, and nothing else: the fake
// classifier standing in for the AI provider must see only the allowlisted
// field, while the actual delivered webhook payload (a real httptest
// server, not the unreachable placeholder baseForm uses) must still
// contain every submitted field, proving delivery is unaffected.
func TestSubmit_SpamFilterIncludeFields(t *testing.T) {
	var deliveredBody []byte
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveredBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	rec := &fakeClassifierRecorder{}

	// discord.Config.Validate() doesn't require https (unlike the webhook
	// channel), so it's the one built-in channel that can point at a plain
	// httptest.NewServer without also needing a TLS test server.
	os.Setenv("TEST_DISCORD_WEBHOOK", webhookSrv.URL)
	t.Cleanup(func() { os.Unsetenv("TEST_DISCORD_WEBHOOK") })

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
security:
  max_body_bytes: 65536
rate_limit:
  backend: memory
  default:
    per_ip: {rate: 100, window: 1m, burst: 100}
    per_form: {rate: 100, window: 1m, burst: 100}
    global: {rate: 1000, window: 1m, burst: 1000}
`)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
fields:
  required: ["name", "email", "message"]
spam_filter:
  enabled: true
  provider:
    type: fake
  include_fields: ["message"]
  on_spam: deliver
  on_error: deliver
channels:
  - id: discord-alerts
    type: discord
    config:
      webhook_url_env: "TEST_DISCORD_WEBHOOK"
      template: "body.tmpl"
`)
	writeFile(t, filepath.Join(dir, "templates", "body.tmpl"),
		`{"name": {{ .Fields.name | json }}, "email": {{ .Fields.email | json }}, "message": {{ .Fields.message | json }}}`)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(discord.Type, discord.New)
	registries.SpamFilter.Register("fake", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return &fakeClassifier{rec: rec}, nil
	})

	a := app.New(filepath.Join(dir, "config.yaml"), registries)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s := &Server{
		App:         a,
		RateLimiter: &fakeRateLimiter{},
		Audit:       audit.New(log),
		Metrics:     metrics.New("test", "test", "test"),
		IDGen:       func() string { return "test-request-id" },
	}

	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}, "message": {"Hello there"}}
	rec2 := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// 1. The fake classifier (standing in for the AI provider) must have
	// seen only the allowlisted field.
	rec.mu.Lock()
	called, classified := rec.called, rec.received
	rec.mu.Unlock()
	if !called {
		t.Fatalf("expected the classifier to have been called")
	}
	if want := map[string]string{"message": "Hello there"}; !equalStringMaps(classified.Fields, want) {
		t.Fatalf("classifier saw Fields = %v, want %v (PII must not reach the AI call)", classified.Fields, want)
	}

	// 2. The actually-delivered webhook payload must still contain every
	// field: include_fields must not affect delivery.
	var delivered map[string]string
	if err := json.Unmarshal(deliveredBody, &delivered); err != nil {
		t.Fatalf("decode delivered webhook body: %v (body: %s)", err, deliveredBody)
	}
	want := map[string]string{"name": "Alice", "email": "alice@example.com", "message": "Hello there"}
	if !equalStringMaps(delivered, want) {
		t.Fatalf("delivered payload = %v, want %v (delivery must not be filtered by include_fields)", delivered, want)
	}
}

// TestSubmit_ResponseModeAsync proves response_mode: async genuinely
// doesn't wait for dispatch: the webhook receiver blocks until explicitly
// released, so if handleSubmit responded before that release, the
// response itself is direct proof it didn't wait for delivery — not just
// "happened to be fast." s.BackgroundWG.Wait() afterward proves the
// deferred work still genuinely completed, just later.
func TestSubmit_ResponseModeAsync(t *testing.T) {
	release := make(chan struct{})
	var deliveredBody []byte
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // held open until the test explicitly lets it through
		deliveredBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSrv.Close()

	os.Setenv("TEST_DISCORD_WEBHOOK_ASYNC", webhookSrv.URL)
	t.Cleanup(func() { os.Unsetenv("TEST_DISCORD_WEBHOOK_ASYNC") })

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
security:
  max_body_bytes: 65536
rate_limit:
  backend: memory
  default:
    per_ip: {rate: 100, window: 1m, burst: 100}
    per_form: {rate: 100, window: 1m, burst: 100}
    global: {rate: 1000, window: 1m, burst: 1000}
`)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
fields:
  required: ["name"]
response_mode: async
channels:
  - id: discord-alerts
    type: discord
    config:
      webhook_url_env: "TEST_DISCORD_WEBHOOK_ASYNC"
      template: "body.tmpl"
`)
	writeFile(t, filepath.Join(dir, "templates", "body.tmpl"), `{"name": {{ .Fields.name | json }}}`)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(discord.Type, discord.New)

	a := app.New(filepath.Join(dir, "config.yaml"), registries)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	var bgWG sync.WaitGroup
	s := &Server{
		App:           a,
		RateLimiter:   &fakeRateLimiter{},
		Audit:         audit.New(log),
		Metrics:       metrics.New("test", "test", "test"),
		IDGen:         func() string { return "test-request-id" },
		BackgroundCtx: context.Background(),
		BackgroundWG:  &bgWG,
	}

	form := url.Values{"name": {"Alice"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 immediately (before the blocked webhook is released), got %d: %s", rec.Code, rec.Body.String())
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatalf("expected success=true even though dispatch hasn't run yet")
	}

	if got := gaugeValue(t, s.Metrics.BackgroundDispatchesInFlight); got != 1 {
		t.Fatalf("formelay_background_dispatches_in_flight = %v, want 1 while the dispatch is still blocked", got)
	}

	close(release)
	s.BackgroundWG.Wait()

	if deliveredBody == nil {
		t.Fatal("expected the background dispatch to have actually delivered after release")
	}
	var delivered map[string]string
	if err := json.Unmarshal(deliveredBody, &delivered); err != nil {
		t.Fatalf("decode delivered webhook body: %v (body: %s)", err, deliveredBody)
	}
	if delivered["name"] != "Alice" {
		t.Fatalf("delivered payload = %v, want name=Alice", delivered)
	}
	if got := gaugeValue(t, s.Metrics.BackgroundDispatchesInFlight); got != 0 {
		t.Fatalf("formelay_background_dispatches_in_flight = %v, want 0 once the dispatch has finished", got)
	}
}

// gaugeValue reads a Gauge's current value directly via its Write method
// (part of client_golang's core prometheus.Metric interface — already a
// direct dependency), rather than pulling in the separate
// prometheus/client_golang/prometheus/testutil subpackage (which drags in
// github.com/kylelemons/godebug as a new transitive dependency) just for
// one value read.
func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

var _ ratelimit.Store = (*fakeRateLimiter)(nil)

// erroringRateLimiter always fails, standing in for a rate-limit backend
// outage (e.g. Valkey unreachable).
type erroringRateLimiter struct{}

func (erroringRateLimiter) Allow(context.Context, string, float64, float64, time.Duration) (bool, error) {
	return false, errors.New("backend unreachable")
}

var _ ratelimit.Store = erroringRateLimiter{}

// TestSubmit_RateLimiterBackendErrorFailsOpen documents a deliberate design
// tradeoff (availability over strictness), not a bug: handleSubmit's rate
// limit checks are `err == nil && !okRL`, so a backend error skips
// enforcement entirely rather than blocking the request. A future change to
// this tradeoff should be a conscious decision, not an accidental
// regression, hence this test.
func TestSubmit_RateLimiterBackendErrorFailsOpen(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	s.RateLimiter = erroringRateLimiter{}
	form := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}}
	rec := doSubmit(t, s, "contact", form, map[string]string{"X-Formelay-Site-Key": "the-key"})
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("rate limiter backend errors are expected to fail open (skip enforcement), got 429")
	}
}

// spamAlwaysClassifier is a fake spamfilter.Classifier that always reports
// spam, used to drive the SpamActionDrop path without a real AI provider.
type spamAlwaysClassifier struct{}

func (spamAlwaysClassifier) Classify(context.Context, render.SubmissionData) (spamfilter.Verdict, error) {
	return spamfilter.Verdict{IsSpam: true, Reason: "test"}, nil
}

// auditCapture is a minimal slog handler that captures each log record's
// top-level attributes as a map, so a test can assert on a specific
// attribute (e.g. "status") without parsing raw JSON output.
type auditCapture struct {
	mu      sync.Mutex
	records []map[string]any
}

func newAuditLogger(cap *auditCapture) *slog.Logger {
	return slog.New(slog.NewJSONHandler(&auditWriter{cap: cap}, nil))
}

// auditWriter implements io.Writer, decoding each JSON log line (one per
// Write call, matching slog's handler behavior) into auditCapture.
type auditWriter struct{ cap *auditCapture }

func (w *auditWriter) Write(p []byte) (int, error) {
	var rec map[string]any
	if err := json.Unmarshal(p, &rec); err == nil {
		w.cap.mu.Lock()
		w.cap.records = append(w.cap.records, rec)
		w.cap.mu.Unlock()
	}
	return len(p), nil
}

func (c *auditCapture) lastStatus(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.records) == 0 {
		t.Fatal("expected at least one audit record")
	}
	status, _ := c.records[len(c.records)-1]["status"].(string)
	return status
}

// TestSubmit_HoneypotAndSpamDropResponsesAreIndistinguishable pins down a
// stated anti-abuse property: a bot must not be able to tell, from the HTTP
// response alone, whether it was caught by the honeypot or by the AI spam
// filter (both must look like an ordinary success), even though the two
// paths are clearly distinguished in the audit log for operators.
func TestSubmit_HoneypotAndSpamDropResponsesAreIndistinguishable(t *testing.T) {
	hpCap := &auditCapture{}
	sHP, _ := newTestServer(t, baseForm)
	sHP.Audit = audit.New(newAuditLogger(hpCap))
	formHP := url.Values{"name": {"Bot"}, "email": {"bot@example.com"}, "website": {"http://spam.example"}}
	recHP := doSubmit(t, sHP, "contact", formHP, map[string]string{"X-Formelay-Site-Key": "the-key"})

	const spamForm = `
id: contact
allowed_origins: ["https://example.com"]
auth:
  site_key: "the-key"
fields:
  required: ["name", "email"]
  validators:
    email: "email"
spam_filter:
  enabled: true
  provider:
    type: fake-always-spam
  on_spam: drop
channels:
  - id: wh
    type: webhook
    config:
      url: "https://example.invalid/hook"
      template: "body.tmpl"
`
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), `
server:
  listen_addr: "127.0.0.1:0"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
security:
  max_body_bytes: 65536
rate_limit:
  backend: memory
  default:
    per_ip: {rate: 100, window: 1m, burst: 100}
    per_form: {rate: 100, window: 1m, burst: 100}
    global: {rate: 1000, window: 1m, burst: 1000}
`)
	writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), spamForm)
	writeFile(t, filepath.Join(dir, "templates", "body.tmpl"), `{"name": {{ .Fields.name | json }}}`)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(webhook.Type, webhook.New)
	registries.SpamFilter.Register("fake-always-spam", func(map[string]any, spamfilter.PromptSource) (spamfilter.Classifier, error) {
		return spamAlwaysClassifier{}, nil
	})
	a := app.New(filepath.Join(dir, "config.yaml"), registries)
	if err := a.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	spamCap := &auditCapture{}
	sSpam := &Server{
		App:         a,
		RateLimiter: &fakeRateLimiter{},
		Audit:       audit.New(newAuditLogger(spamCap)),
		Metrics:     metrics.New("test", "test", "test"),
		IDGen:       func() string { return "test-request-id" },
	}
	formSpam := url.Values{"name": {"Alice"}, "email": {"alice@example.com"}}
	recSpam := doSubmit(t, sSpam, "contact", formSpam, map[string]string{"X-Formelay-Site-Key": "the-key"})

	if recHP.Code != http.StatusOK || recSpam.Code != http.StatusOK {
		t.Fatalf("expected both paths to respond 200, got honeypot=%d spam=%d", recHP.Code, recSpam.Code)
	}
	if recHP.Body.String() != recSpam.Body.String() {
		t.Fatalf("responses must be indistinguishable: honeypot body=%s, spam body=%s", recHP.Body.String(), recSpam.Body.String())
	}

	hpStatus, spamStatus := hpCap.lastStatus(t), spamCap.lastStatus(t)
	if hpStatus != "spam_dropped_honeypot" {
		t.Fatalf("honeypot audit status = %q, want spam_dropped_honeypot", hpStatus)
	}
	if spamStatus != "spam_dropped_ai" {
		t.Fatalf("spam-filter audit status = %q, want spam_dropped_ai", spamStatus)
	}
}
