package api

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/render"
)

const channelSendTimeout = 10 * time.Second

// dispatchNormal renders each channel's own subject/body templates and
// sends concurrently.
func (s *Server) dispatchNormal(ctx context.Context, formID string, channels map[string]*app.CompiledChannel, data render.SubmissionData) []audit.ChannelResult {
	var wg sync.WaitGroup
	ch := make(chan audit.ChannelResult, len(channels))
	for id, cc := range channels {
		wg.Add(1)
		go func(id string, cc *app.CompiledChannel) {
			defer wg.Done()
			ch <- s.sendOne(ctx, formID, id, cc, buildMessage(cc, data))
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
func (s *Server) dispatchShared(ctx context.Context, formID string, cf *app.CompiledForm, channelIDs []string, tmpl *render.Template, data render.SubmissionData) []audit.ChannelResult {
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
			ch <- s.sendOne(ctx, formID, id, cc, msg)
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

func (s *Server) sendOne(ctx context.Context, formID, channelID string, cc *app.CompiledChannel, msg notify.RenderedMessage) audit.ChannelResult {
	if errMsg, ok := msg.Meta["render_error"]; ok {
		s.Metrics.DeliveriesTotal.WithLabelValues(formID, channelID, cc.Notifier.Type(), "failure").Inc()
		return audit.ChannelResult{ID: channelID, Type: cc.Notifier.Type(), Success: false, Error: "render: " + errMsg}
	}
	cctx, cancel := context.WithTimeout(ctx, channelSendTimeout)
	defer cancel()
	dstart := time.Now()
	err := cc.Notifier.Send(cctx, msg)
	s.Metrics.DeliveryLatencySeconds.WithLabelValues(formID, cc.Notifier.Type()).Observe(time.Since(dstart).Seconds())
	status, errStr := "success", ""
	if err != nil {
		status, errStr = "failure", err.Error()
	}
	s.Metrics.DeliveriesTotal.WithLabelValues(formID, channelID, cc.Notifier.Type(), status).Inc()
	return audit.ChannelResult{ID: channelID, Type: cc.Notifier.Type(), Success: err == nil, Error: errStr}
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
