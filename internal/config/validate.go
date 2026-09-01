package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/bossm8/formelay/internal/yamlutil"
)

// RegexValidatorPrefix marks a fields.validators kind as a custom regex
// pattern rather than a built-in name — e.g. "regex:^\d{5}$". Exported so
// internal/api's runValidator (the actual matcher) uses the exact same
// prefix this package validates the syntax of at config load.
const RegexValidatorPrefix = "regex:"

// ValidateGlobal performs structural validation of the global config.
func ValidateGlobal(g *GlobalConfig) error {
	if g.Server.ListenAddr == "" {
		return fmt.Errorf("server.listen_addr is required")
	}
	switch g.RateLimit.Backend {
	case "memory":
	case "valkey":
		if len(g.RateLimit.Valkey.Addresses) == 0 {
			return fmt.Errorf("rate_limit.valkey.addresses is required when rate_limit.backend is 'valkey'")
		}
		if g.RateLimit.Valkey.OnError != "" && g.RateLimit.Valkey.OnError != "allow" && g.RateLimit.Valkey.OnError != "deny" {
			return fmt.Errorf("rate_limit.valkey.on_error must be 'allow' or 'deny'")
		}
		if g.RateLimit.Valkey.PasswordEnv != "" && os.Getenv(g.RateLimit.Valkey.PasswordEnv) == "" {
			return fmt.Errorf("rate_limit.valkey.password_env: environment variable %q is not set", g.RateLimit.Valkey.PasswordEnv)
		}
	default:
		return fmt.Errorf("rate_limit.backend must be 'memory' or 'valkey', got %q", g.RateLimit.Backend)
	}
	for name, b := range g.RateLimit.OutboundBuckets {
		if err := validateRateLimitFields(b.Rate, b.Burst, b.Window, b.OnLimit, fmt.Sprintf("rate_limit.outbound_buckets[%q]", name)); err != nil {
			return err
		}
	}
	if g.FormsDir == "" {
		return fmt.Errorf("forms_dir is required")
	}
	if g.Server.TLS.Enabled {
		if g.Server.TLS.CertFile == "" || g.Server.TLS.KeyFile == "" {
			return fmt.Errorf("server.tls.cert_file and server.tls.key_file are required when server.tls.enabled is true")
		}
		if _, err := os.Stat(g.Server.TLS.CertFile); err != nil {
			return fmt.Errorf("server.tls.cert_file: %w", err)
		}
		if _, err := os.Stat(g.Server.TLS.KeyFile); err != nil {
			return fmt.Errorf("server.tls.key_file: %w", err)
		}
	}
	return nil
}

// ValidateForm performs structural validation of one form's config.
// channelIDs is the set of this form's own channel ids, used to cross-check
// spam_filter.route references.
func ValidateForm(f *FormConfig) error {
	if f.Auth.SiteKey == "" {
		return fmt.Errorf("form %q: auth.site_key is required", f.ID)
	}
	switch f.Auth.Transport {
	case "", "header":
	case "form_field":
		if f.Auth.FormFieldName == "" {
			return fmt.Errorf("form %q: auth.form_field_name is required when transport is 'form_field'", f.ID)
		}
	default:
		return fmt.Errorf("form %q: auth.transport must be 'header' or 'form_field'", f.ID)
	}

	if f.Captcha.Enabled {
		if f.Captcha.Provider == "" {
			return fmt.Errorf("form %q: captcha.provider is required when captcha is enabled", f.ID)
		}
		if f.Captcha.SecretEnv == "" {
			return fmt.Errorf("form %q: captcha.secret_env is required when captcha is enabled", f.ID)
		}
		if f.Captcha.ResponseField == "" {
			return fmt.Errorf("form %q: captcha.response_field is required when captcha is enabled", f.ID)
		}
		switch f.Captcha.OnError {
		case "", "fail_open", "fail_closed":
		default:
			return fmt.Errorf("form %q: captcha.on_error must be 'fail_open' or 'fail_closed'", f.ID)
		}
	}

	channelIDs := map[string]bool{}
	for _, ch := range f.Channels {
		if ch.ID == "" {
			return fmt.Errorf("form %q: every channel needs an 'id'", f.ID)
		}
		if channelIDs[ch.ID] {
			return fmt.Errorf("form %q: duplicate channel id %q", f.ID, ch.ID)
		}
		channelIDs[ch.ID] = true
		if ch.Type == "" {
			return fmt.Errorf("form %q: channel %q: 'type' is required", f.ID, ch.ID)
		}
		if err := validateOutboundRateLimit(ch.RateLimit, fmt.Sprintf("channel %q", ch.ID), f.ID); err != nil {
			return err
		}
	}

	if f.SpamFilter.Enabled {
		if f.SpamFilter.Provider.Type == "" {
			return fmt.Errorf("form %q: spam_filter.provider.type is required when spam_filter is enabled", f.ID)
		}
		if err := validateSpamAction("on_spam", f.SpamFilter.OnSpam, f.ID); err != nil {
			return err
		}
		if err := validateSpamAction("on_error", f.SpamFilter.OnError, f.ID); err != nil {
			return err
		}
		if f.SpamFilter.OnSpam == SpamActionRoute && f.SpamFilter.Route.SpamTemplate == "" {
			return fmt.Errorf("form %q: spam_filter.route.spam_template is required when on_spam is 'route'", f.ID)
		}
		if f.SpamFilter.OnError == SpamActionRoute && f.SpamFilter.Route.ErrorTemplate == "" && f.SpamFilter.Route.SpamTemplate == "" {
			return fmt.Errorf("form %q: spam_filter.route.error_template (or spam_template as a fallback) is required when on_error is 'route'", f.ID)
		}
		for _, id := range f.SpamFilter.Route.SpamChannels {
			if !channelIDs[id] {
				return fmt.Errorf("form %q: spam_filter.route.spam_channels references unknown channel id %q", f.ID, id)
			}
		}
		for _, id := range f.SpamFilter.Route.ErrorChannels {
			if !channelIDs[id] {
				return fmt.Errorf("form %q: spam_filter.route.error_channels references unknown channel id %q", f.ID, id)
			}
		}
		if err := validateOutboundRateLimit(f.SpamFilter.RateLimit, "spam_filter", f.ID); err != nil {
			return err
		}
	}

	switch f.ChannelsRequired {
	case "", "any", "all", "none":
	default:
		return fmt.Errorf("form %q: channels_required must be 'any', 'all', or 'none'", f.ID)
	}

	switch f.ResponseMode {
	case "", "sync", "async":
	default:
		return fmt.Errorf("form %q: response_mode must be 'sync' or 'async'", f.ID)
	}

	for name, kind := range f.Fields.Validators {
		if err := validateFieldValidatorKind(kind); err != nil {
			return fmt.Errorf("form %q: fields.validators[%q]: %w", f.ID, name, err)
		}
	}

	return nil
}

// validateFieldValidatorKind checks kind is one of the built-in validator
// names, or a syntactically valid "regex:<pattern>" — an unrecognized kind
// used to silently no-op at runtime (a typo like "emial" validated
// nothing, with no warning); it's now a config-load error instead.
func validateFieldValidatorKind(kind string) error {
	switch kind {
	case "email", "url", "notblank":
		return nil
	}
	if pattern, ok := strings.CutPrefix(kind, RegexValidatorPrefix); ok {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unknown validator %q (must be 'email', 'url', 'notblank', or '%s<pattern>')", kind, RegexValidatorPrefix)
}

// validateOutboundRateLimit checks rl (a channel's or spam_filter's
// rate_limit block); a nil rl (the default, no limiting configured) is
// always valid. what names the block being validated for the error
// message, e.g. `channel "email-owner"` or "spam_filter".
//
// shared_key and the inline rate/window/burst/on_limit/max_wait fields
// are mutually exclusive (see the OutboundRateLimitConfig doc comment):
// a shared_key block is only checked for that exclusivity here — its
// actual numbers are resolved from the referenced global bucket by
// resolveOutboundRateLimits, once every form has been individually
// validated.
func validateOutboundRateLimit(rl *OutboundRateLimitConfig, what, formID string) error {
	if rl == nil {
		return nil
	}
	if rl.SharedKey != "" {
		if rl.Rate != 0 || rl.Window.Std() != 0 || rl.Burst != 0 || rl.OnLimit != "" || rl.MaxWait.Std() != 0 {
			return fmt.Errorf("form %q: %s: rate_limit.shared_key is mutually exclusive with rate/window/burst/on_limit/max_wait — define those once in the global rate_limit.outbound_buckets entry instead", formID, what)
		}
		return nil
	}
	return validateRateLimitFields(rl.Rate, rl.Burst, rl.Window, rl.OnLimit, fmt.Sprintf("form %q: %s", formID, what))
}

// validateRateLimitFields checks the four numeric/enum fields shared by
// both an inline OutboundRateLimitConfig and a named
// OutboundBucketConfig. what prefixes the error message, already
// carrying whatever context (form/block, or bucket name) is relevant.
func validateRateLimitFields(rate, burst float64, window yamlutil.Duration, onLimit, what string) error {
	switch onLimit {
	case "", "wait", "fail":
	default:
		return fmt.Errorf("%s: rate_limit.on_limit must be 'wait' or 'fail'", what)
	}
	if rate <= 0 || window.Std() <= 0 || burst <= 0 {
		return fmt.Errorf("%s: rate_limit.rate, .window, and .burst must all be > 0", what)
	}
	return nil
}

// resolveOutboundRateLimits fills in every shared_key-referencing
// OutboundRateLimitConfig (across every form's channels and spam_filter)
// with the numbers from its named global bucket. Must run after every
// form has already passed ValidateForm (which only checks a shared_key
// block's own exclusivity, not that the name it references exists —
// that cross-references the global config, which a single form's
// validation never sees). Mutates rl in place; SharedKey itself is left
// set so outboundRateLimitKey (internal/api/dispatch.go) still pools
// every reference to the same name into one bucket key.
func resolveOutboundRateLimits(global *GlobalConfig, forms map[string]*FormConfig) error {
	resolve := func(rl *OutboundRateLimitConfig, what, formID string) error {
		if rl == nil || rl.SharedKey == "" {
			return nil
		}
		bucket, ok := global.RateLimit.OutboundBuckets[rl.SharedKey]
		if !ok {
			return fmt.Errorf("form %q: %s: rate_limit.shared_key %q is not defined in rate_limit.outbound_buckets", formID, what, rl.SharedKey)
		}
		rl.Rate, rl.Window, rl.Burst, rl.OnLimit, rl.MaxWait = bucket.Rate, bucket.Window, bucket.Burst, bucket.OnLimit, bucket.MaxWait
		return nil
	}

	for id, f := range forms {
		for _, ch := range f.Channels {
			if err := resolve(ch.RateLimit, fmt.Sprintf("channel %q", ch.ID), id); err != nil {
				return err
			}
		}
		if err := resolve(f.SpamFilter.RateLimit, "spam_filter", id); err != nil {
			return err
		}
	}
	return nil
}

func validateSpamAction(field string, action SpamAction, formID string) error {
	switch action {
	case "", SpamActionDeliver, SpamActionDeliverTagged, SpamActionDrop, SpamActionRoute:
		return nil
	default:
		return fmt.Errorf("form %q: spam_filter.%s must be one of deliver, deliver_tagged, drop, route", formID, field)
	}
}
