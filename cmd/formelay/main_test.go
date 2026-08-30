package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/ratelimit/memory"
	"github.com/bossm8/formelay/internal/yamlutil"
)

func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLogLevel(in); got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBuildLoggerChoosesFormat proves logging.format genuinely selects the
// handler (not just that some default always wins): JSON output starts
// with '{', slog's text handler doesn't and uses key=value pairs instead.
func TestBuildLoggerChoosesFormat(t *testing.T) {
	var jsonBuf bytes.Buffer
	buildLogger(config.LoggingConfig{Format: "json"}, &jsonBuf).Info("hello")
	if !strings.HasPrefix(strings.TrimSpace(jsonBuf.String()), "{") {
		t.Fatalf("expected JSON output, got: %s", jsonBuf.String())
	}

	var textBuf bytes.Buffer
	buildLogger(config.LoggingConfig{Format: "text"}, &textBuf).Info("hello")
	if strings.HasPrefix(strings.TrimSpace(textBuf.String()), "{") {
		t.Fatalf("expected non-JSON (text) output, got: %s", textBuf.String())
	}
	if !strings.Contains(textBuf.String(), "msg=hello") {
		t.Fatalf("expected slog's text handler shape, got: %s", textBuf.String())
	}
}

func TestBuildLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := buildLogger(config.LoggingConfig{Level: "warn", Format: "json"}, &buf)

	log.Info("should be filtered out")
	if buf.Len() != 0 {
		t.Fatalf("expected Info-level records to be filtered out when logging.level is 'warn', got: %s", buf.String())
	}

	log.Warn("should pass")
	if buf.Len() == 0 {
		t.Fatalf("expected Warn-level records to pass when logging.level is 'warn'")
	}
}

func TestNonZero(t *testing.T) {
	cases := []struct {
		name     string
		d, def   time.Duration
		wantSame bool // true => want d back, false => want def back
	}{
		{"zero falls back to default", 0, 5 * time.Second, false},
		{"positive value passes through", 3 * time.Second, 5 * time.Second, true},
		{"negative value falls back to default too", -1 * time.Second, 5 * time.Second, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nonZero(c.d, c.def)
			want := c.def
			if c.wantSame {
				want = c.d
			}
			if got != want {
				t.Fatalf("nonZero(%v, %v) = %v, want %v", c.d, c.def, got, want)
			}
		})
	}
}

// TestBuildRateLimiterMemoryBackend covers the default/"memory" branch,
// which needs no live external service (unlike "valkey").
func TestBuildRateLimiterMemoryBackend(t *testing.T) {
	m := metrics.New("test", "test", "test")

	t.Run("default backend builds a memory.Store with defaulted idle/cleanup", func(t *testing.T) {
		st, closeFn, err := buildRateLimiter(config.RateLimitConfig{}, m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer closeFn()
		if _, ok := st.(*memory.Store); !ok {
			t.Fatalf("expected a *memory.Store, got %T", st)
		}
		if closeFn == nil {
			t.Fatal("expected a non-nil close function")
		}
	})

	t.Run("explicit memory backend with configured idle/cleanup", func(t *testing.T) {
		cfg := config.RateLimitConfig{
			Backend:         "memory",
			BucketIdleTTL:   yamlutil.Duration(time.Minute),
			CleanupInterval: yamlutil.Duration(30 * time.Second),
		}
		st, closeFn, err := buildRateLimiter(cfg, m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer closeFn()
		if _, ok := st.(*memory.Store); !ok {
			t.Fatalf("expected a *memory.Store, got %T", st)
		}
	})

	t.Run("close function is safe to call", func(t *testing.T) {
		_, closeFn, err := buildRateLimiter(config.RateLimitConfig{}, m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		closeFn() // must not panic
	})
}
