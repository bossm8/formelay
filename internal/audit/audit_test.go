package audit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestLoggerEnabled(t *testing.T) {
	var buf bytes.Buffer
	l := New(newTestLogger(&buf))
	l.Log(Event{RequestID: "r1", FormID: "contact", Status: "success"}, true, false)
	if buf.Len() == 0 {
		t.Fatalf("expected a log record to be emitted when enabled")
	}
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("record is not valid JSON: %v (record: %s)", err, buf.String())
	}
	if record["form_id"] != "contact" {
		t.Fatalf("form_id = %v, want %q", record["form_id"], "contact")
	}
}

func TestLoggerDisabledIsANoOp(t *testing.T) {
	var buf bytes.Buffer
	l := New(newTestLogger(&buf))
	l.Log(Event{RequestID: "r1", FormID: "contact", Status: "success", FieldValues: map[string]string{"email": "alice@example.com"}}, false, true)
	if buf.Len() != 0 {
		t.Fatalf("expected no output when the logger is disabled, got: %s", buf.String())
	}
}

func TestLoggerFieldValuesGating(t *testing.T) {
	fields := map[string]string{"email": "alice@example.com", "message": "hi"}

	t.Run("log_field_values false omits field values even when the event carries them", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(newTestLogger(&buf))
		l.Log(Event{RequestID: "r1", FormID: "contact", Status: "success", FieldValues: fields}, true, false)
		if bytes.Contains(buf.Bytes(), []byte("alice@example.com")) {
			t.Fatalf("PII leaked into the audit log despite log_field_values=false: %s", buf.String())
		}
	})

	t.Run("log_field_values true includes field values", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(newTestLogger(&buf))
		l.Log(Event{RequestID: "r1", FormID: "contact", Status: "success", FieldValues: fields}, true, true)
		if !bytes.Contains(buf.Bytes(), []byte("alice@example.com")) {
			t.Fatalf("expected field values in the audit log when log_field_values=true, got: %s", buf.String())
		}
	})

	t.Run("no field values on the event means nothing to include either way", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(newTestLogger(&buf))
		l.Log(Event{RequestID: "r1", FormID: "contact", Status: "origin_denied"}, true, true)
		var record map[string]any
		if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
			t.Fatalf("record is not valid JSON: %v", err)
		}
		if _, ok := record["fields"]; ok {
			t.Fatalf("did not expect a 'fields' key when Event.FieldValues was empty")
		}
	})
}
