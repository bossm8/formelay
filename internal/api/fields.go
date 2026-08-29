package api

import (
	"fmt"
	"net/mail"
	"net/url"

	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/sanitize"
)

const defaultMaxFieldLength = 5000

// sanitizeFields normalizes every field value (UTF-8, control chars, length
// cap) — see plan Security Model on why this is not HTML sanitization.
func sanitizeFields(fields map[string]string, cfg config.FieldsConfig) (map[string]string, error) {
	maxLen := cfg.MaxFieldLength
	if maxLen <= 0 {
		maxLen = defaultMaxFieldLength
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		clean, err := sanitize.Field(v, true, maxLen)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		out[k] = clean
	}
	return out, nil
}

// validateFields checks required presence and named validators, returning
// the list of field names that failed (empty slice = all valid).
func validateFields(fields map[string]string, cfg config.FieldsConfig) []string {
	var failed []string
	for _, name := range cfg.Required {
		if fields[name] == "" {
			failed = append(failed, name)
		}
	}
	for name, kind := range cfg.Validators {
		v, present := fields[name]
		if !present || v == "" {
			continue // required-ness already checked above
		}
		if !runValidator(kind, v) {
			failed = append(failed, name)
		}
	}
	return failed
}

func runValidator(kind, value string) bool {
	switch kind {
	case "email":
		_, err := mail.ParseAddress(value)
		return err == nil
	case "url":
		u, err := url.Parse(value)
		return err == nil && u.Scheme != "" && u.Host != ""
	case "notblank":
		return value != ""
	default:
		return true
	}
}
