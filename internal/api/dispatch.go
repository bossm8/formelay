package api

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
)

const channelSendTimeout = 10 * time.Second

// outboundPollInterval is how often awaitOutboundToken re-checks the
// bucket while waiting (on_limit: wait). A package var so tests can shrink
// it instead of waiting on the real interval.
var outboundPollInterval = 200 * time.Millisecond

const defaultOutboundMaxWait = 5 * time.Second

// dispatchNormal renders each channel's own subject/body templates and
// sends concurrently.
func (s *Server) dispatchNormal(ctx context.Context, requestID, formID string, channels map[string]*app.CompiledChannel, data render.SubmissionData) []audit.ChannelResult {
	var wg sync.WaitGroup
	ch := make(chan audit.ChannelResult, len(channels))
	for id, cc := range channels {
		wg.Add(1)
		go func(id string, cc *app.CompiledChannel) {
			defer wg.Done()
			ch <- s.sendOne(ctx, requestID, formID, id, cc, buildMessage(cc, data))
		}(id, cc)
	}
	wg.Wait()
	close(ch)
	var results []audit.ChannelResult
	for r := range ch {
		results = append(results, r)
	}
	return results
}

// dispatchShared renders one template once and sends the identical payload
// to every listed channel — used for spam-review routing, where all target
// channels share a single review template rather than their own.
func (s *Server) dispatchShared(ctx context.Context, requestID, formID string, cf *app.CompiledForm, channelIDs []string, tmpl *render.Template, data render.SubmissionData) []audit.ChannelResult {
	if tmpl == nil {
		return nil
	}
	body, err := tmpl.Execute(data)
	if err != nil {
		results := make([]audit.ChannelResult, 0, len(channelIDs))
		for _, id := range channelIDs {
			results = append(results, audit.ChannelResult{ID: id, Success: false, Error: "render: " + err.Error()})
		}
		return results
	}

	var wg sync.WaitGroup
	ch := make(chan audit.ChannelResult, len(channelIDs))
	for _, id := range channelIDs {
		cc, ok := cf.Channels[id]
		if !ok {
			ch <- audit.ChannelResult{ID: id, Success: false, Error: "channel not found or disabled"}
			continue
		}
		wg.Add(1)
		go func(id string, cc *app.CompiledChannel) {
			defer wg.Done()
			msg := notify.RenderedMessage{
				Subject: "[Spam review] " + cf.Config.DisplayName,
				Body:    body, ContentType: "application/json",
			}
			ch <- s.sendOne(ctx, requestID, formID, id, cc, msg)
		}(id, cc)
	}
	wg.Wait()
	close(ch)
	var results []audit.ChannelResult
	for r := range ch {
		results = append(results, r)
	}
	return results
}

func buildMessage(cc *app.CompiledChannel, data render.SubmissionData) notify.RenderedMessage {
	msg := notify.RenderedMessage{Meta: map[string]string{}}
	if rtp, ok := cc.Notifier.(notify.ReplyToFieldProvider); ok {
		if field := rtp.ReplyToField(); field != "" {
			if v := data.Fields[field]; v != "" {
				msg.Meta["reply_to"] = v
			}
		}
	}
	keys := make([]string, 0, len(cc.Templates))
	for key := range cc.Templates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rendered, err := cc.Templates[key].Execute(data)
		if err != nil {
			msg.Body = nil
			msg.Meta["render_error"] = err.Error()
			break
		}
		switch key {
		case "subject":
			msg.Subject = string(rendered)
		default:
			msg.Body = rendered
			msg.ContentType = cc.ContentType[key]
		}
	}
	return msg
}

func (s *Server) sendOne(ctx context.Context, requestID, formID, channelID string, cc *app.CompiledChannel, msg notify.RenderedMessage) audit.ChannelResult {
	if errMsg, ok := msg.Meta["render_error"]; ok {
		slog.Debug("channel template render failed, not sending", "request_id", requestID, "form_id", formID, "channel", channelID, "error", errMsg)
		s.Metrics.DeliveriesTotal.WithLabelValues(formID, channelID, cc.Notifier.Type(), "failure").Inc()
		return audit.ChannelResult{ID: channelID, Type: cc.Notifier.Type(), Success: false, Error: "render: " + errMsg}
	}
	if cc.RateLimit != nil {
		key := outboundRateLimitKey("channel:"+formID+":"+channelID, cc.RateLimit)
		waited, allowed, err := s.awaitOutboundToken(ctx, key, cc.RateLimit)
		if waited > 0 {
			s.Metrics.RatelimitOutboundWaitSeconds.WithLabelValues(formID, channelID).Observe(waited.Seconds())
		}
		if err == nil && !allowed {
			slog.Debug("channel outbound rate limit exceeded, not sending", "request_id", requestID, "form_id", formID, "channel", channelID, "waited_ms", waited.Milliseconds())
			s.Metrics.DeliveriesTotal.WithLabelValues(formID, channelID, cc.Notifier.Type(), "rate_limited").Inc()
			return audit.ChannelResult{ID: channelID, Type: cc.Notifier.Type(), Success: false, Error: "rate_limited: outbound quota exceeded"}
		}
		// err != nil (rate-limit backend error): fail open, same convention
		// handleSubmit already uses for inbound limiting.
	}
	slog.Debug("sending to channel", "request_id", requestID, "form_id", formID, "channel", channelID, "type", cc.Notifier.Type())
	cctx, cancel := context.WithTimeout(ctx, channelSendTimeout)
	defer cancel()
	dstart := time.Now()
	err := cc.Notifier.Send(cctx, msg)
	elapsed := time.Since(dstart)
	s.Metrics.DeliveryLatencySeconds.WithLabelValues(formID, cc.Notifier.Type()).Observe(elapsed.Seconds())
	status, errStr := "success", ""
	if err != nil {
		status, errStr = "failure", err.Error()
		slog.Debug("channel send failed", "request_id", requestID, "form_id", formID, "channel", channelID, "type", cc.Notifier.Type(), "latency_ms", elapsed.Milliseconds(), "error", errStr)
	} else {
		slog.Debug("channel send succeeded", "request_id", requestID, "form_id", formID, "channel", channelID, "type", cc.Notifier.Type(), "latency_ms", elapsed.Milliseconds())
	}
	s.Metrics.DeliveriesTotal.WithLabelValues(formID, channelID, cc.Notifier.Type(), status).Inc()
	return audit.ChannelResult{ID: channelID, Type: cc.Notifier.Type(), Success: err == nil, Error: errStr}
}

// outboundRateLimitKey scopes a bucket by defaultKey (e.g. a specific
// channel in a specific form, or a form's spam filter), unless
// rl.SharedKey is set — in which case every caller using the same
// SharedKey (a channel or the spam filter, in any form) draws from one
// shared bucket, for outbound calls that genuinely hit one real-world
// quota (e.g. two email channels through the same SMTP account, or a
// channel and the spam filter both billed against the same provider
// account).
func outboundRateLimitKey(defaultKey string, rl *config.OutboundRateLimitConfig) string {
	if rl.SharedKey != "" {
		return "outbound-shared:" + rl.SharedKey
	}
	return defaultKey
}

// awaitOutboundToken checks (and, for on_limit: wait, polls) rl's bucket
// under key (see outboundRateLimitKey). waited is only non-zero once
// actual polling happened, so a normal immediately-allowed call never
// touches the wait-time histogram. Uses ctx directly, not
// channelSendTimeout — a configured wait shouldn't eat into a network
// send's own timeout budget.
func (s *Server) awaitOutboundToken(ctx context.Context, key string, rl *config.OutboundRateLimitConfig) (waited time.Duration, allowed bool, err error) {
	rate, burst, window := rl.Rate, rl.Burst, rl.Window.Std()

	allowed, err = s.RateLimiter.Allow(ctx, key, rate, burst, window)
	if err != nil || allowed || rl.OnLimit == "fail" {
		return 0, allowed, err
	}

	maxWait := rl.MaxWait.Std()
	if maxWait <= 0 {
		maxWait = defaultOutboundMaxWait
	}
	start := time.Now()
	deadline := start.Add(maxWait)
	for {
		select {
		case <-ctx.Done():
			return time.Since(start), false, ctx.Err()
		case <-time.After(outboundPollInterval):
		}
		allowed, err = s.RateLimiter.Allow(ctx, key, rate, burst, window)
		if err != nil || allowed || time.Now().After(deadline) {
			return time.Since(start), allowed, err
		}
	}
}

func anySuccess(results []audit.ChannelResult) bool {
	if len(results) == 0 {
		return true
	}
	for _, r := range results {
		if r.Success {
			return true
		}
	}
	return false
}

func allSuccess(results []audit.ChannelResult) bool {
	for _, r := range results {
		if !r.Success {
			return false
		}
	}
	return true
}
