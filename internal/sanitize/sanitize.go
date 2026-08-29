// Package sanitize normalizes attacker-controlled form field values before
// they reach any template or header. The primary XSS/injection defense is
// still contextual output encoding (html/template, the `json` template
// func) done elsewhere — this package is a defense-in-depth layer that
// strips markup outright and closes the remaining gaps: invalid UTF-8,
// control characters, Unicode normalization, oversized values, and
// header-unsafe values.
//
// Markup stripping uses bluemonday (github.com/microcosm-cc/bluemonday) —
// the de facto standard Go HTML sanitizer (used by Gitea, Hugo, and many
// others), built on golang.org/x/net/html's spec-compliant tokenizer rather
// than a regex or hand-rolled parser. StrictPolicy() is used deliberately:
// every field here is always plain text data, never a safe-HTML-subset
// feature, so nothing is ever allowed through, only stripped.
//
// Unicode/control-character normalization runs through golang.org/x/text's
// transform pipeline (runes.Remove + unicode/norm), for the same reason —
// a real transformer/parser interface instead of ad hoc string building.
// golang.org/x/text is already an indirect dependency of this module (via
// the Prometheus client) and is maintained by the Go team.
package sanitize

import (
	"errors"
	"fmt"
	"html"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ErrInvalidUTF8 is returned when a field value is not valid UTF-8.
var ErrInvalidUTF8 = errors.New("sanitize: invalid UTF-8")

// stripMarkup is a package-level bluemonday policy — safe for concurrent
// use, and cheap enough to build once rather than per call.
var stripMarkup = bluemonday.StrictPolicy()

// controlCharFilter builds a transform.Transformer that removes Unicode
// control characters (category Cc — including things like ANSI escape
// sequences and NUL bytes, which have no legitimate place in a form field
// and are a classic log/terminal-injection vector), preserving tab and,
// when multiline is true, newline.
func controlCharFilter(multiline bool) transform.Transformer {
	return runes.Remove(runes.Predicate(func(r rune) bool {
		if !unicode.IsControl(r) {
			return false // keep
		}
		if r == '\t' {
			return false // keep
		}
		if r == '\n' && multiline {
			return false // keep
		}
		return true // remove
	}))
}

// Field normalizes a single field value:
//   - rejects invalid UTF-8 outright
//   - strips any HTML/script markup via bluemonday's StrictPolicy
//   - normalizes CRLF/CR line endings to LF
//   - Unicode-normalizes to NFC (canonical composition), so visually
//     identical text can't hide behind different byte representations
//   - strips control characters (tab, and newline when multiline, kept)
//   - truncates to maxLen runes (0 = no limit)
//
// bluemonday's Sanitize output is HTML-entity-escaped (safe to embed
// directly in an HTML document) — but this value goes on to several
// different output contexts, each with its own escaping (html/template's
// auto-escaping for the email body, the `json` template func for
// Discord/webhook/AI-prompt targets), so passing already-escaped text
// through those would double-escape it and corrupt plain text like
// "5 < 10" into "5 &lt; 10". html.UnescapeString reverses bluemonday's
// escaping; this is safe because bluemonday has already *removed* any
// actually dangerous tags/attributes by this point — what's left is
// ordinary text, and every downstream render path re-escapes based on the
// string's content at render time regardless of what this function did.
func Field(value string, multiline bool, maxLen int) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalidUTF8
	}

	value = html.UnescapeString(stripMarkup.Sanitize(value))

	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	if !multiline {
		value = strings.ReplaceAll(value, "\n", " ")
	}

	out, _, err := transform.String(transform.Chain(norm.NFC, controlCharFilter(multiline)), value)
	if err != nil {
		return "", fmt.Errorf("sanitize: normalize: %w", err)
	}

	if maxLen > 0 {
		r := []rune(out)
		if len(r) > maxLen {
			out = string(r[:maxLen])
		}
	}
	return out, nil
}

// HeaderSafeAddress validates value as a single well-formed email address
// suitable for use in an email header (e.g. Reply-To sourced from a
// submitted field), using net/mail's RFC 5322 parser rather than a regex.
// Unlike Field, this rejects rather than strips — a forged value with
// embedded CRLF will not parse as a valid address, which is what actually
// prevents SMTP header injection (see plan Security Model).
func HeaderSafeAddress(value string) (string, error) {
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}
