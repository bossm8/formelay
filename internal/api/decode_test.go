package api

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeSubmission(t *testing.T) {
	t.Run("urlencoded happy path", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=Alice&email=alice%40example.com"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		got, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["name"][0] != "Alice" || got["email"][0] != "alice@example.com" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("JSON happy path", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Alice","email":"alice@example.com"}`))
		r.Header.Set("Content-Type", "application/json")
		got, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["name"][0] != "Alice" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("JSON with a non-string value is rejected (flat string fields only)", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":{"nested":"object"}}`))
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20); err == nil {
			t.Fatal("expected error for a nested JSON value")
		}
	})

	t.Run("multipart text fields", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.WriteField("name", "Alice"); err != nil {
			t.Fatal(err)
		}
		w.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &buf)
		r.Header.Set("Content-Type", w.FormDataContentType())
		got, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["name"][0] != "Alice" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("multipart with a file part is rejected", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, err := w.CreateFormFile("attachment", "evil.exe")
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte("binary content"))
		w.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &buf)
		r.Header.Set("Content-Type", w.FormDataContentType())
		_, err = decodeSubmission(httptest.NewRecorder(), r, 1<<20)
		if !errors.Is(err, ErrFileUpload) {
			t.Fatalf("expected ErrFileUpload, got %v", err)
		}
	})

	t.Run("oversized urlencoded body is rejected, not a panic", func(t *testing.T) {
		big := strings.Repeat("a", 1000)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name="+big))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 100); err == nil {
			t.Fatal("expected an error for a body exceeding maxBodyBytes")
		}
	})

	t.Run("oversized multipart body is rejected, not a panic", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.WriteField("name", strings.Repeat("a", 1000)); err != nil {
			t.Fatal(err)
		}
		w.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &buf)
		r.Header.Set("Content-Type", w.FormDataContentType())
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 100); err == nil {
			t.Fatal("expected an error for a body exceeding maxBodyBytes")
		}
	})

	t.Run("oversized JSON body is rejected, not a panic", func(t *testing.T) {
		big := strings.Repeat("a", 1000)
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"`+big+`"}`))
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 100); err == nil {
			t.Fatal("expected an error for a body exceeding maxBodyBytes")
		}
	})

	t.Run("unsupported content type is rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("<xml/>"))
		r.Header.Set("Content-Type", "application/xml")
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20); err == nil {
			t.Fatal("expected error for unsupported Content-Type")
		}
	})

	t.Run("missing content type is rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=Alice"))
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20); err == nil {
			t.Fatal("expected error for missing Content-Type")
		}
	})

	t.Run("malformed JSON body is rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{not valid json`))
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeSubmission(httptest.NewRecorder(), r, 1<<20); err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})
}

func TestFlatten(t *testing.T) {
	multi := map[string][]string{
		"single":   {"a"},
		"repeated": {"first", "second"},
		"empty":    {},
	}
	got := flatten(multi)
	if got["single"] != "a" {
		t.Fatalf("single = %q", got["single"])
	}
	if got["repeated"] != "first" {
		t.Fatalf("repeated = %q, want first value to win", got["repeated"])
	}
	if _, ok := got["empty"]; ok {
		t.Fatalf("expected no entry for a key with zero values, got %q", got["empty"])
	}
}
