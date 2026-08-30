package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	respondSuccess(rec, "req-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.RequestID != "req-1" || body.Error != "" {
		t.Fatalf("body = %+v", body)
	}
}

func TestRespondError(t *testing.T) {
	rec := httptest.NewRecorder()
	respondError(rec, http.StatusBadRequest, "validation_failed", "req-2")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Success || body.Error != "validation_failed" || body.RequestID != "req-2" {
		t.Fatalf("body = %+v", body)
	}
}

// TestRespondErrorDoesNotLeakInternals guards the pipeline's stated
// property that every error path responds with a stable, generic error
// code, never a raw Go error string (which could reveal file paths, driver
// internals, or other implementation details to an attacker).
func TestRespondErrorDoesNotLeakInternals(t *testing.T) {
	rec := httptest.NewRecorder()
	respondError(rec, http.StatusInternalServerError, "invalid_body", "req-3")

	body := rec.Body.String()
	for _, leak := range []string{"/internal/", ".go:", "runtime error", "panic"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response body %q must not contain internal-looking text %q", body, leak)
		}
	}
}
