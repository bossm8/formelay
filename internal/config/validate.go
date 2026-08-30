package config

import (
	"fmt"
	"os"
)

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
	default:
		return fmt.Errorf("rate_limit.backend must be 'memory' or 'valkey', got %q", g.RateLimit.Backend)
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
	}

	switch f.ChannelsRequired {
	case "", "any", "all", "none":
	default:
		return fmt.Errorf("form %q: channels_required must be 'any', 'all', or 'none'", f.ID)
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
