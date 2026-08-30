package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/bossm8/formelay/internal/config"
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
