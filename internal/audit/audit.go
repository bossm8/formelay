// Package audit emits structured, JSON audit-log records for every
// submission attempt. Field values (PII) are omitted by default — see plan
// Security Model.
package audit

import (
	"log/slog"
	"time"
)

// ChannelResult records one delivery attempt's outcome.
type ChannelResult struct {
	ID      string
	Type    string
	Success bool
	Error   string
}

// Event is one submission's full audit record.
type Event struct {
	RequestID   string
	FormID      string
	SourceIP    string
	Origin      string
	Status      string // success | validation_failed | origin_denied | auth_denied | rate_limited | spam_dropped_honeypot | captcha_failed | spam_dropped_ai | delivery_failed
	SpamVerdict string // "", spam, not_spam, error
	SpamAction  string // "", deliver, deliver_tagged, drop, route
	Channels    []ChannelResult
	Latency     time.Duration
	FieldValues map[string]string // only included if the caller passes logFieldValues=true to Log
}

// Logger emits Events as structured slog records.
type Logger struct {
	log *slog.Logger
}

func New(log *slog.Logger) *Logger {
	return &Logger{log: log}
}

// Log emits e, unless enabled is false (logging.audit.enabled), in which
// case it's a no-op. logFieldValues (logging.audit.log_field_values)
// decides whether e.FieldValues is actually included in the output; the
// caller always passes whatever it has on FieldValues regardless, so the
// privacy decision lives entirely here, not scattered across call sites.
//
// Both flags are parameters, not constructor-time state deliberately: this
// codebase always reads config fresh from the current snapshot on every
// request (see api.Server.handleSubmit's rt := s.App.Current()), so
// logging.audit.* changes take effect on the very next request after a hot
// reload, the same as every other per-form/global setting, rather than
// only at process startup.
func (l *Logger) Log(e Event, enabled, logFieldValues bool) {
	if !enabled {
		return
	}
	attrs := []any{
		"type", "audit",
		"request_id", e.RequestID,
		"form_id", e.FormID,
		"source_ip", e.SourceIP,
		"origin", e.Origin,
		"status", e.Status,
		"latency_ms", e.Latency.Milliseconds(),
	}
	if e.SpamVerdict != "" {
		attrs = append(attrs, "spam_verdict", e.SpamVerdict, "spam_action", e.SpamAction)
	}
	if len(e.Channels) > 0 {
		channels := make([]map[string]any, 0, len(e.Channels))
		for _, c := range e.Channels {
			channels = append(channels, map[string]any{
				"id": c.ID, "type": c.Type, "success": c.Success, "error": c.Error,
			})
		}
		attrs = append(attrs, "channels", channels)
	}
	if logFieldValues && len(e.FieldValues) > 0 {
		attrs = append(attrs, "fields", e.FieldValues)
	}
	l.log.Info("submission", attrs...)
}
