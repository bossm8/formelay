package api

import (
	"net/http"
	"strings"
)

// DangerousDisableOriginCheck is a reserved allowed_origins entry that
// disables origin checking entirely for a form — including a submission
// with no Origin header at all, unlike every other entry type. Development
// only: it removes one of formelay's real defense layers (see README
// Security Model), and every reload while it's set logs a loud warning
// (see cmd/formelay/main.go's formsWithOriginCheckDisabled) so it can't
// stay on unnoticed. Named this way on purpose — nobody should enable it
// without knowing exactly what it does.
const DangerousDisableOriginCheck = "DANGEROUS_DISABLED"

// originAllowed reports whether origin matches one of allowed, supporting
// an exact match, a "https://*.example.com" wildcard-subdomain entry, or
// DangerousDisableOriginCheck (matches anything, including no Origin
// header at all).
func originAllowed(allowed []string, origin string) bool {
	for _, a := range allowed {
		if a == DangerousDisableOriginCheck {
			return true
		}
	}
	if origin == "" {
		return false
	}
	for _, a := range allowed {
		if a == origin {
			return true
		}
		if strings.Contains(a, "*.") {
			scheme, rest, ok := strings.Cut(a, "://")
			if !ok {
				continue
			}
			suffix, ok := strings.CutPrefix(rest, "*.")
			if !ok {
				continue
			}
			originScheme, originRest, ok := strings.Cut(origin, "://")
			if !ok || originScheme != scheme {
				continue
			}
			if originRest == suffix || strings.HasSuffix(originRest, "."+suffix) {
				return true
			}
		}
	}
	return false
}

// writeCORSHeaders echoes Access-Control-Allow-Origin only when origin
// matched the form's allowlist — never a bare "*" when an allowlist exists.
// The one exception: origin == "" can only reach here via
// DangerousDisableOriginCheck (every other path already rejects an empty
// origin before calling this), and there's no real origin to echo, so a
// literal "*" is written instead.
func writeCORSHeaders(w http.ResponseWriter, origin, headerName string) {
	acao := origin
	if acao == "" {
		acao = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", acao)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", headerName+", Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
}
