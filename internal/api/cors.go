package api

import (
	"net/http"
	"strings"
)

// originAllowed reports whether origin matches one of allowed, supporting
// an exact match or a "https://*.example.com" wildcard-subdomain entry.
func originAllowed(allowed []string, origin string) bool {
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
func writeCORSHeaders(w http.ResponseWriter, origin, headerName string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", headerName+", Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
}
