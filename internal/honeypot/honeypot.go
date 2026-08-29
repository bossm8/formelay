// Package honeypot implements the free, zero-network-call spam trap: a
// configurable hidden form field that a human never fills in.
package honeypot

// Triggered reports whether the honeypot field was filled in.
func Triggered(fields map[string]string, fieldName string) bool {
	if fieldName == "" {
		return false
	}
	v, ok := fields[fieldName]
	return ok && v != ""
}
