// Package metrics defines all Prometheus collectors, registered against a
// dedicated registry (not the global default) exposed only on the internal
// metrics listener — never on the public submission port.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Metrics struct {
	Registry *prometheus.Registry

	SubmissionsTotal             *prometheus.CounterVec
	DeliveriesTotal              *prometheus.CounterVec
	DeliveryLatencySeconds       *prometheus.HistogramVec
	RateLimitedTotal             *prometheus.CounterVec
	HoneypotTriggeredTotal       *prometheus.CounterVec
	CaptchaVerificationsTotal    *prometheus.CounterVec
	SpamFilterVerdictsTotal      *prometheus.CounterVec
	SpamFilterActionsTotal       *prometheus.CounterVec
	SpamFilterLatencySeconds     *prometheus.HistogramVec
	ConfigReloadTotal            *prometheus.CounterVec
	ConfigLastReloadTimestamp    prometheus.Gauge
	RatelimitBucketsActive       *prometheus.GaugeVec
	RatelimitBackendErrorsTotal  *prometheus.CounterVec
	RatelimitOutboundWaitSeconds *prometheus.HistogramVec
	HTTPRequestsInFlight         prometheus.Gauge
	BackgroundDispatchesInFlight prometheus.Gauge
	BuildInfo                    *prometheus.GaugeVec
}

func New(version, commit, goVersion string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		Registry: reg,
		SubmissionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_submissions_total", Help: "Submission attempts by form and outcome.",
		}, []string{"form", "status"}),
		DeliveriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_deliveries_total", Help: "Channel delivery attempts.",
		}, []string{"form", "channel", "channel_type", "status"}),
		DeliveryLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "formelay_delivery_latency_seconds", Help: "Channel delivery latency.",
			Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"form", "channel_type"}),
		RateLimitedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_rate_limited_total", Help: "Requests rejected by rate limiting.",
		}, []string{"form", "scope"}),
		HoneypotTriggeredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_honeypot_triggered_total", Help: "Submissions dropped by the honeypot check.",
		}, []string{"form"}),
		CaptchaVerificationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_captcha_verifications_total", Help: "CAPTCHA verification attempts.",
		}, []string{"form", "provider", "status"}),
		SpamFilterVerdictsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_spam_filter_verdicts_total", Help: "AI spam-filter verdicts.",
		}, []string{"form", "verdict"}),
		SpamFilterActionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_spam_filter_actions_total", Help: "Resolved on_spam/on_error action taken.",
		}, []string{"form", "trigger", "action"}),
		SpamFilterLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "formelay_spam_filter_latency_seconds", Help: "AI spam-filter call latency.",
		}, []string{"form"}),
		ConfigReloadTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_config_reload_total", Help: "Config reload attempts.",
		}, []string{"status"}),
		ConfigLastReloadTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "formelay_config_last_reload_timestamp_seconds", Help: "Unix timestamp of the last successful config reload.",
		}),
		RatelimitBucketsActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "formelay_ratelimit_buckets_active", Help: "Active in-memory rate-limit buckets (memory backend only).",
		}, []string{"scope"}),
		RatelimitBackendErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "formelay_ratelimit_backend_errors_total", Help: "Rate-limit backend connectivity/timeout errors.",
		}, []string{"backend"}),
		RatelimitOutboundWaitSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "formelay_ratelimit_outbound_wait_seconds", Help: "Time spent waiting for an outbound channel rate-limit token (on_limit: wait).",
			Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"form", "channel"}),
		HTTPRequestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "formelay_http_requests_in_flight", Help: "In-flight HTTP requests.",
		}),
		BackgroundDispatchesInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "formelay_background_dispatches_in_flight", Help: "response_mode: async submissions whose spam-filter/dispatch is still running in the background.",
		}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "formelay_build_info", Help: "Build metadata.",
		}, []string{"version", "commit", "go_version"}),
	}

	reg.MustRegister(
		m.SubmissionsTotal, m.DeliveriesTotal, m.DeliveryLatencySeconds, m.RateLimitedTotal,
		m.HoneypotTriggeredTotal, m.CaptchaVerificationsTotal, m.SpamFilterVerdictsTotal,
		m.SpamFilterActionsTotal, m.SpamFilterLatencySeconds, m.ConfigReloadTotal,
		m.ConfigLastReloadTimestamp, m.RatelimitBucketsActive, m.RatelimitBackendErrorsTotal,
		m.RatelimitOutboundWaitSeconds, m.HTTPRequestsInFlight, m.BackgroundDispatchesInFlight, m.BuildInfo,
	)
	m.BuildInfo.WithLabelValues(version, commit, goVersion).Set(1)
	return m
}
