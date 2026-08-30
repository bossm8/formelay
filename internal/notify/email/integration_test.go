//go:build integration

// Run against a real Mailpit instance:
//
//	docker compose -f docker-compose.test.yml up -d mailpit
//	SMTP_TEST_HOST=127.0.0.1 SMTP_TEST_PORT=1025 SMTP_TEST_API=http://127.0.0.1:8025 \
//	  go test -tags=integration ./internal/notify/email/... -v
//
// or via `make test-integration`, which drives the whole thing through
// docker-compose so no local Mailpit is needed.
//
// This proves go-mail's actual SMTP wire protocol is accepted end-to-end by
// a real (if disposable) server — internal/notify/email/email_test.go's
// unit tests only cover Send's early-return validation paths, since there's
// no real SMTP conversation to have without a server to talk to.
package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/yamlutil"
)

func smtpTestHost() string {
	if v := os.Getenv("SMTP_TEST_HOST"); v != "" {
		return v
	}
	return "127.0.0.1"
}

func smtpTestPort(t *testing.T) int {
	t.Helper()
	v := os.Getenv("SMTP_TEST_PORT")
	if v == "" {
		return 1025
	}
	port, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("invalid SMTP_TEST_PORT %q: %v", v, err)
	}
	return port
}

func mailpitAPI() string {
	if v := os.Getenv("SMTP_TEST_API"); v != "" {
		return v
	}
	return "http://127.0.0.1:8025"
}

// mailpitAddress mirrors net/mail.Address's JSON shape, as returned by
// Mailpit's API (github.com/axllent/mailpit internal/storage.Message uses
// *net/mail.Address fields with no json tags, so the wire field names are
// the Go field names verbatim: "Name" and "Address").
type mailpitAddress struct {
	Name    string
	Address string
}

// mailpitMessage mirrors the subset of Mailpit's storage.Message this test
// cares about (see GET /api/v1/message/{ID} in Mailpit's apiv1 package).
type mailpitMessage struct {
	From    mailpitAddress
	To      []mailpitAddress
	ReplyTo []mailpitAddress
	Subject string
	Text    string
	HTML    string
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

// latestMailpitMessage polls Mailpit's "latest" convenience endpoint until a
// message is available (Send() has already returned by the time this is
// called, meaning the SMTP DATA command completed and Mailpit acknowledged
// it, but indexing/API visibility can lag by a beat) or the timeout elapses.
func latestMailpitMessage(t *testing.T) mailpitMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(mailpitAPI() + "/api/v1/message/latest")
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			lastErr = fmt.Errorf("no message yet (404)")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		var msg mailpitMessage
		if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
			t.Fatalf("decode mailpit message: %v", err)
		}
		return msg
	}
	t.Fatalf("no message appeared in mailpit within the timeout: %v", lastErr)
	return mailpitMessage{}
}

func TestSendDeliversViaRealSMTP(t *testing.T) {
	resetMailpit(t)
	t.Cleanup(func() { resetMailpit(t) })

	n := &emailNotifier{cfg: Config{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Host:     smtpTestHost(),
		Port:     smtpTestPort(t),
		BodyType: "text",
		Timeout:  yamlutil.Duration(5 * time.Second),
	}}

	msg := notify.RenderedMessage{
		Subject: "Integration test subject",
		Body:    []byte("Hello from the integration test."),
		Meta:    map[string]string{"reply_to": "reply-to@example.com"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := latestMailpitMessage(t)
	if got.From.Address != "sender@example.com" {
		t.Errorf("From = %q, want sender@example.com", got.From.Address)
	}
	if len(got.To) != 1 || got.To[0].Address != "recipient@example.com" {
		t.Errorf("To = %v, want [recipient@example.com]", got.To)
	}
	if len(got.ReplyTo) != 1 || got.ReplyTo[0].Address != "reply-to@example.com" {
		t.Errorf("ReplyTo = %v, want [reply-to@example.com]", got.ReplyTo)
	}
	if got.Subject != "Integration test subject" {
		t.Errorf("Subject = %q, want %q", got.Subject, "Integration test subject")
	}
	// go-mail's MIME encoding appends a trailing CRLF to the plain-text
	// part; TrimSpace so the assertion is about content, not transport
	// framing — a real detail this integration test exists to surface, but
	// not itself the property under test.
	if strings.TrimSpace(got.Text) != "Hello from the integration test." {
		t.Errorf("Text body = %q, want %q", got.Text, "Hello from the integration test.")
	}
}

func TestSendHTMLBodyType(t *testing.T) {
	resetMailpit(t)
	t.Cleanup(func() { resetMailpit(t) })

	n := &emailNotifier{cfg: Config{
		From:     "sender@example.com",
		To:       []string{"recipient@example.com"},
		Host:     smtpTestHost(),
		Port:     smtpTestPort(t),
		BodyType: "html",
		Timeout:  yamlutil.Duration(5 * time.Second),
	}}

	msg := notify.RenderedMessage{
		Subject: "HTML body test",
		Body:    []byte("<p>Hello <strong>there</strong></p>"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := latestMailpitMessage(t)
	if got.HTML == "" {
		t.Fatalf("expected an HTML body, got empty HTML field (raw message: %+v)", got)
	}
	if len(got.ReplyTo) != 0 {
		t.Fatalf("expected no Reply-To when Meta[reply_to] is unset, got %v", got.ReplyTo)
	}
}
