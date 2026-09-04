package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bossm8/formelay/internal/app"
)

// TestNewMuxRouting drives real routing (mux.ServeHTTP), unlike
// dispatch_test.go/submit_test.go which call handleSubmit directly — this
// is the one place the actual path pattern / method matching registered in
// NewMux is exercised.
func TestNewMuxRouting(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	mux := s.NewMux()

	t.Run("POST /f/{formID}/submit reaches handleSubmit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/f/contact/submit", strings.NewReader("name=Alice&email=alice@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("X-Formelay-Site-Key", "the-key")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("expected routing to reach handleSubmit, got 404")
		}
	})

	t.Run("OPTIONS /f/{formID}/submit reaches handlePreflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/f/contact/submit", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 from preflight, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/f/contact/submit", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("unknown path is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nope", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestNewInternalMuxLivenessReadiness(t *testing.T) {
	s, _ := newTestServer(t, baseForm)
	mux := s.NewInternalMux("/healthz", "/readyz", "", nil)

	t.Run("liveness always 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("readiness 200 once a runtime is loaded", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("readiness 503 when App.Current() is nil", func(t *testing.T) {
		unloaded := &Server{App: app.New("/nonexistent/config.yaml", app.Registries{})}
		mux := unloaded.NewInternalMux("/healthz", "/readyz", "", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
	})
}

func TestNewInternalMuxReload(t *testing.T) {
	s, _ := newTestServer(t, baseForm)

	t.Run("POST calls the injected reload func and returns success", func(t *testing.T) {
		called := false
		mux := s.NewInternalMux("/healthz", "/readyz", "/reload", func() error {
			called = true
			return nil
		})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/reload", nil))
		if !called {
			t.Fatal("expected the reload func to be called")
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"success":true`) {
			t.Fatalf("expected a success:true body, got %s", rec.Body.String())
		}
	})

	t.Run("a reload error is reported as 500 with the error in the body", func(t *testing.T) {
		mux := s.NewInternalMux("/healthz", "/readyz", "/reload", func() error {
			return errors.New("config: validate form \"contact\": boom")
		})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/reload", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "boom") {
			t.Fatalf("expected the reload error in the response body, got %s", rec.Body.String())
		}
	})

	t.Run("an empty reloadPath registers no route at all", func(t *testing.T) {
		mux := s.NewInternalMux("/healthz", "/readyz", "", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/reload", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when reload.handle_http is disabled, got %d", rec.Code)
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		mux := s.NewInternalMux("/healthz", "/readyz", "/reload", func() error { return nil })
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reload", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandlePreflight(t *testing.T) {
	s, _ := newTestServer(t, baseForm)

	preflight := func(t *testing.T, formID, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/f/"+formID+"/submit", nil)
		req.SetPathValue("formID", formID)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		s.handlePreflight(rec, req)
		return rec
	}

	t.Run("unknown form is 404", func(t *testing.T) {
		rec := preflight(t, "nope", "https://example.com")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("disallowed origin is 403", func(t *testing.T) {
		rec := preflight(t, "contact", "https://evil.example")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("allowed origin gets 204 with CORS headers, no auth/rate-limit enforcement", func(t *testing.T) {
		rec := preflight(t, "contact", "https://example.com")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
			t.Fatalf("Access-Control-Allow-Origin = %q", got)
		}
	})

	t.Run("preflight is never rate limited, even after exhausting the limiter", func(t *testing.T) {
		s.RateLimiter = &fakeRateLimiter{denyAll: true}
		t.Cleanup(func() { s.RateLimiter = &fakeRateLimiter{} })
		rec := preflight(t, "contact", "https://example.com")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected preflight to bypass rate limiting entirely, got %d", rec.Code)
		}
	})
}
