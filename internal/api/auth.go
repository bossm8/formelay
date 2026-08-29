package api

import (
	"crypto/subtle"
	"net/http"

	"github.com/bossm8/formelay/internal/config"
)

const defaultSiteKeyHeader = "X-Formelay-Site-Key"

// extractSiteKey pulls the submitted site key from the header or form field
// configured for this form.
func extractSiteKey(r *http.Request, fields map[string]string, auth config.AuthConfig) string {
	if auth.Transport == "form_field" {
		return fields[auth.FormFieldName]
	}
	header := auth.HeaderName
	if header == "" {
		header = defaultSiteKeyHeader
	}
	return r.Header.Get(header)
}

// siteKeyValid does a constant-time comparison. The site key is a public
// capability token, not a secret (see README Security Model), but
// constant-time comparison still costs nothing and avoids timing side
// channels as a matter of habit.
func siteKeyValid(submitted, expected string) bool {
	if submitted == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(expected)) == 1
}
