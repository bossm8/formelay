package api

import (
	"net/http"
	"time"

	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/honeypot"
	"github.com/bossm8/formelay/internal/render"
)

// handleSubmit implements the submission pipeline described in the plan:
// resolve form -> origin -> rate limit -> decode -> site key -> sanitize/
// validate -> honeypot -> captcha -> AI spam filter -> render/dispatch.
// (The site key is checked after decode, not before, so a form configured
// for `transport: form_field` can extract it from the decoded body; this
// is the one deliberate reordering versus the plan's listed step numbers,
// and does not change any security property.)
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := s.IDGen()
	formID := r.PathValue("formID")
	ip := clientIP(r, s.TrustedProxies)
	origin := r.Header.Get("Origin")

	rt := s.App.Current()
	if rt == nil {
		respondError(w, http.StatusServiceUnavailable, "not_ready", requestID)
		return
	}
	global := rt.Config.Global
	cf, ok := rt.Forms[formID]
	if !ok || !cf.Config.IsEnabled() {
		http.NotFound(w, r)
		return
	}
	fc := cf.Config

	// Declared here (not via := at the decode step below) so the finish
	// closure can see whatever value fields holds at call time, nil before
	// decode, the actual submission after. Every audit call site passes it
	// unconditionally along with the live logging.audit.log_field_values
	// value (read from global, not baked into the Logger at startup, so a
	// hot reload of that setting takes effect on the next request); Log
	// itself decides whether to actually include it.
	var fields map[string]string

	finish := func(status, code string, httpStatus int) {
		s.Metrics.SubmissionsTotal.WithLabelValues(formID, status).Inc()
		s.Audit.Log(audit.Event{RequestID: requestID, FormID: formID, SourceIP: ip, Origin: origin, Status: status, Latency: time.Since(start), FieldValues: fields}, global.Logging.Audit.Enabled, global.Logging.Audit.LogFieldValues)
		respondError(w, httpStatus, code, requestID)
	}

	// 1. origin
	allowed := originAllowed(fc.AllowedOrigins, origin)
	headerName := fc.Auth.HeaderName
	if headerName == "" {
		headerName = defaultSiteKeyHeader
	}
	if allowed {
		writeCORSHeaders(w, origin, headerName)
	} else {
		finish("origin_denied", "origin_not_allowed", http.StatusForbidden)
		return
	}

	ctx := r.Context()

	// 2. rate limit: global, per-IP, per-form
	globalRule := global.RateLimit.Default.Global
	if okRL, err := s.RateLimiter.Allow(ctx, "global", globalRule.Rate, globalRule.Burst, globalRule.Window.Std()); err == nil && !okRL {
		s.Metrics.RateLimitedTotal.WithLabelValues(formID, "global").Inc()
		finish("rate_limited", "rate_limited", http.StatusTooManyRequests)
		return
	}
	ipRule, formRule := global.RateLimit.Default.PerIP, global.RateLimit.Default.PerForm
	if fc.RateLimit != nil {
		if fc.RateLimit.PerIP != nil {
			ipRule = *fc.RateLimit.PerIP
		}
		if fc.RateLimit.PerForm != nil {
			formRule = *fc.RateLimit.PerForm
		}
	}
	if okRL, err := s.RateLimiter.Allow(ctx, "ip:"+formID+":"+ip, ipRule.Rate, ipRule.Burst, ipRule.Window.Std()); err == nil && !okRL {
		s.Metrics.RateLimitedTotal.WithLabelValues(formID, "per_ip").Inc()
		finish("rate_limited", "rate_limited", http.StatusTooManyRequests)
		return
	}
	if okRL, err := s.RateLimiter.Allow(ctx, "form:"+formID, formRule.Rate, formRule.Burst, formRule.Window.Std()); err == nil && !okRL {
		s.Metrics.RateLimitedTotal.WithLabelValues(formID, "per_form").Inc()
		finish("rate_limited", "rate_limited", http.StatusTooManyRequests)
		return
	}

	// 3. decode
	multi, err := decodeSubmission(w, r, global.Security.MaxBodyBytes)
	if err != nil {
		finish("validation_failed", "invalid_body", http.StatusBadRequest)
		return
	}
	fields = flatten(multi)

	// 4. site key
	if !siteKeyValid(extractSiteKey(r, fields, fc.Auth), fc.Auth.SiteKey) {
		finish("auth_denied", "invalid_site_key", http.StatusUnauthorized)
		return
	}

	// 5. sanitize + validate
	fields, err = sanitizeFields(fields, fc.Fields)
	if err != nil {
		finish("validation_failed", "invalid_field_encoding", http.StatusBadRequest)
		return
	}
	if failed := validateFields(fields, fc.Fields); len(failed) > 0 {
		finish("validation_failed", "validation_failed", http.StatusBadRequest)
		return
	}

	// 6. honeypot
	if honeypot.Triggered(fields, fc.Honeypot.FieldName) {
		s.Metrics.HoneypotTriggeredTotal.WithLabelValues(formID).Inc()
		s.Metrics.SubmissionsTotal.WithLabelValues(formID, "spam_dropped_honeypot").Inc()
		s.Audit.Log(audit.Event{RequestID: requestID, FormID: formID, SourceIP: ip, Origin: origin, Status: "spam_dropped_honeypot", Latency: time.Since(start), FieldValues: fields}, global.Logging.Audit.Enabled, global.Logging.Audit.LogFieldValues)
		respondSuccess(w, requestID)
		return
	}

	data := render.SubmissionData{
		Form:        render.FormMeta{ID: fc.ID, DisplayName: fc.DisplayName},
		Fields:      fields,
		FieldsMulti: multi,
		Meta:        render.RequestMeta{RequestID: requestID, Timestamp: time.Now(), SourceIP: ip, Origin: origin},
	}

	// 7. captcha
	if fc.Captcha.Enabled && cf.CaptchaVerifier != nil {
		token := fields[fc.Captcha.ResponseField]
		passed, verr := cf.CaptchaVerifier.Verify(ctx, token, ip)
		status := "success"
		if verr != nil {
			status = "error"
			onError := fc.Captcha.OnError
			if onError == "" {
				onError = "fail_closed"
			}
			passed = onError == "fail_open"
		} else if !passed {
			status = "failed"
		}
		s.Metrics.CaptchaVerificationsTotal.WithLabelValues(formID, fc.Captcha.Provider, status).Inc()
		if !passed {
			finish("captcha_failed", "captcha_failed", http.StatusBadRequest)
			return
		}
	}

	// 8. AI spam filter
	targetChannels := cf.Channels
	var sharedTemplate *render.Template
	var routeChannelIDs []string

	if fc.SpamFilter.Enabled && cf.SpamClassifier != nil {
		// A filtered *copy* for the classifier only: data itself (used
		// below for delivery and the human-facing spam-review template)
		// must keep every field regardless of include_fields.
		classifyData := data.WithFieldsLimitedTo(fc.SpamFilter.IncludeFields)
		sfStart := time.Now()
		verdict, cerr := cf.SpamClassifier.Classify(ctx, classifyData)
		s.Metrics.SpamFilterLatencySeconds.WithLabelValues(formID).Observe(time.Since(sfStart).Seconds())

		var trigger string
		var action config.SpamAction
		switch {
		case cerr != nil:
			trigger = "error"
			s.Metrics.SpamFilterVerdictsTotal.WithLabelValues(formID, "error").Inc()
			action = fc.SpamFilter.OnError
			data.Meta.SpamFilterErr = cerr.Error()
		case verdict.IsSpam:
			trigger = "spam"
			s.Metrics.SpamFilterVerdictsTotal.WithLabelValues(formID, "spam").Inc()
			action = fc.SpamFilter.OnSpam
			data.Meta.SpamReason = verdict.Reason
		default:
			s.Metrics.SpamFilterVerdictsTotal.WithLabelValues(formID, "not_spam").Inc()
		}

		if trigger != "" {
			if action == "" {
				action = config.SpamActionDeliver
			}
			s.Metrics.SpamFilterActionsTotal.WithLabelValues(formID, trigger, string(action)).Inc()

			switch action {
			case config.SpamActionDrop:
				status := "spam_dropped_ai"
				s.Metrics.SubmissionsTotal.WithLabelValues(formID, status).Inc()
				s.Audit.Log(audit.Event{RequestID: requestID, FormID: formID, SourceIP: ip, Origin: origin, Status: status, SpamVerdict: trigger, SpamAction: string(action), Latency: time.Since(start), FieldValues: fields}, global.Logging.Audit.Enabled, global.Logging.Audit.LogFieldValues)
				respondSuccess(w, requestID)
				return
			case config.SpamActionRoute:
				data.Meta.SpamSuspected = true
				routeChannelIDs = fc.SpamFilter.Route.SpamChannels
				templateKey := "spam"
				if trigger == "error" {
					routeChannelIDs = fc.SpamFilter.Route.ErrorChannels
					templateKey = "error"
				}
				if len(routeChannelIDs) == 0 {
					status := "spam_dropped_ai"
					s.Metrics.SubmissionsTotal.WithLabelValues(formID, status).Inc()
					s.Audit.Log(audit.Event{RequestID: requestID, FormID: formID, SourceIP: ip, Origin: origin, Status: status, SpamVerdict: trigger, SpamAction: string(action), Latency: time.Since(start), FieldValues: fields}, global.Logging.Audit.Enabled, global.Logging.Audit.LogFieldValues)
					respondSuccess(w, requestID)
					return
				}
				sharedTemplate = cf.SpamRouteTemplates[templateKey]
				targetChannels = nil
			case config.SpamActionDeliverTagged:
				data.Meta.SpamSuspected = true
			case config.SpamActionDeliver:
				// fall through to normal dispatch, unchanged
			}
		}
	}

	// 9. render + dispatch
	var results []audit.ChannelResult
	if sharedTemplate != nil {
		results = s.dispatchShared(ctx, formID, cf, routeChannelIDs, sharedTemplate, data)
	} else {
		results = s.dispatchNormal(ctx, formID, targetChannels, data)
	}

	required := fc.ChannelsRequired
	if required == "" {
		required = "any"
	}
	ok = true
	switch required {
	case "all":
		ok = allSuccess(results)
	case "none":
		ok = true
	default:
		ok = anySuccess(results)
	}

	status := "success"
	if !ok {
		status = "delivery_failed"
	}
	s.Metrics.SubmissionsTotal.WithLabelValues(formID, status).Inc()
	s.Audit.Log(audit.Event{RequestID: requestID, FormID: formID, SourceIP: ip, Origin: origin, Status: status, Channels: results, Latency: time.Since(start), FieldValues: fields}, global.Logging.Audit.Enabled, global.Logging.Audit.LogFieldValues)

	if ok {
		respondSuccess(w, requestID)
	} else {
		respondError(w, http.StatusBadGateway, "delivery_failed", requestID)
	}
}
