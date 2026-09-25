package api

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"

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

// validateFields checks required presence and named validators. Required-
// field failures are always loud (there's no silent variant — silence
// only makes sense for a validator kind, not "you left this blank").
// A validator kind failure is silent (resolved like the honeypot, not a
// normal validation_failed) if and only if its kind string carries the
// config.SilentValidatorPrefix modifier — see
// config.StripValidatorModifiers.
func validateFields(fields map[string]string, cfg config.FieldsConfig) (loud, silent []string) {
	for _, name := range cfg.Required {
		if fields[name] == "" {
			loud = append(loud, name)
		}
	}
	for name, kind := range cfg.Validators {
		v, present := fields[name]
		if !present || v == "" {
			continue // required-ness already checked above
		}
		if !runValidator(kind, v) {
			_, _, isSilent := config.StripValidatorModifiers(kind)
			if isSilent {
				silent = append(silent, name)
			} else {
				loud = append(loud, name)
			}
		}
	}
	return loud, silent
}

// runValidator strips any not:/silent: modifiers (see
// config.StripValidatorModifiers) and dispatches to the base kind,
// negating the result when not: was present. silent-ness is a reporting
// concern for the caller, not something the check itself needs to know.
func runValidator(kind, value string) bool {
	base, negate, _ := config.StripValidatorModifiers(kind)
	result := runBaseValidator(base, value)
	if negate {
		return !result
	}
	return result
}

func runBaseValidator(kind, value string) bool {
	switch {
	case kind == "email":
		_, err := mail.ParseAddress(value)
		return err == nil
	case kind == "url":
		u, err := url.Parse(value)
		return err == nil && u.Scheme != "" && u.Host != ""
	case kind == "notblank":
		return value != ""
	case strings.HasPrefix(kind, config.RegexValidatorPrefix):
		re, err := compiledRegex(strings.TrimPrefix(kind, config.RegexValidatorPrefix))
		if err != nil {
			// config.ValidateForm rejects an invalid pattern at load time,
			// so a published config never reaches this; runBaseValidator
			// is also exercised directly by tests with arbitrary kind
			// strings, so fail closed here rather than panic.
			return false
		}
		return re.MatchString(value)
	default:
		return true
	}
}

var (
	regexCacheMu sync.RWMutex
	regexCache   = map[string]*regexp.Regexp{}
)

// compiledRegex compiles pattern once and caches it by pattern text — safe
// to share across every form/field/request, since the same pattern string
// always compiles to the same stateless, immutable matcher.
func compiledRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCacheMu.Lock()
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}
