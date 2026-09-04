// Package api implements the public HTTP submission endpoint and the
// internal health/metrics endpoints.
package api

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/ratelimit"
)

type Server struct {
	App            *app.App
	RateLimiter    ratelimit.Store
	Audit          *audit.Logger
	Metrics        *metrics.Metrics
	IDGen          func() string
	TrustedProxies []*net.IPNet
	// BackgroundCtx and BackgroundWG back response_mode: async — a
	// server-lifetime context (not r.Context(), which net/http cancels
	// the instant handleSubmit returns) and a WaitGroup so
	// cmd/formelay/main.go's shutdown sequence can drain in-flight
	// background dispatches before the process exits. Harmless/unused
	// when no form ever sets response_mode: async.
	BackgroundCtx context.Context
	BackgroundWG  *sync.WaitGroup
}

// NewMux builds the public-facing mux: the submission endpoint only.
// Liveness/readiness deliberately live on the internal listener alongside
// /metrics (see NewInternalMux), not on the public port. Wrapped with an
// in-flight-request gauge, since this is the attacker-facing surface the
// metric exists to watch — the internal listener isn't instrumented.
func (s *Server) NewMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /f/{formID}/submit", s.handleSubmit)
	mux.HandleFunc("OPTIONS /f/{formID}/submit", s.handlePreflight)
	return s.withInFlightGauge(mux)
}

func (s *Server) withInFlightGauge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Metrics.HTTPRequestsInFlight.Inc()
		defer s.Metrics.HTTPRequestsInFlight.Dec()
		next.ServeHTTP(w, r)
	})
}

// NewInternalMux builds the internal-only mux: liveness/readiness, and a
// reload trigger when reloadPath is non-empty (empty means reload.
// handle_http is disabled — see cmd/formelay). The caller adds /metrics
// to the same mux (see cmd/formelay).
func (s *Server) NewInternalMux(livenessPath, readinessPath, reloadPath string, reload func() error) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+livenessPath, s.handleLiveness)
	mux.HandleFunc("GET "+readinessPath, s.handleReadiness)
	if reloadPath != "" {
		mux.HandleFunc("POST "+reloadPath, s.handleReload(reload))
	}
	return mux
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if s.App.Current() == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleReload(reload func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := reload(); err != nil {
			respondJSON(w, http.StatusInternalServerError, response{Success: false, Error: err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, response{Success: true})
	}
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	rt := s.App.Current()
	if rt == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	formID := r.PathValue("formID")
	cf, ok := rt.Forms[formID]
	if !ok || !cf.Config.IsEnabled() {
		http.NotFound(w, r)
		return
	}
	origin := r.Header.Get("Origin")
	if !originAllowed(cf.Config.AllowedOrigins, origin) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	headerName := cf.Config.Auth.HeaderName
	if headerName == "" {
		headerName = defaultSiteKeyHeader
	}
	writeCORSHeaders(w, origin, headerName)
	w.WriteHeader(http.StatusNoContent)
}
