// Package captcha verifies CAPTCHA/challenge response tokens against a
// provider's verify endpoint. A single generic implementation covers Turnstile,
// hCaptcha, reCAPTCHA v2/v3 and most other providers via config alone (see
// generic.go); the Registry exists for the rare provider that needs bespoke
// Go code.
package captcha

import (
	"context"
	"fmt"
)

// Verifier checks a CAPTCHA response token server-side.
type Verifier interface {
	Verify(ctx context.Context, responseToken, remoteIP string) (bool, error)
}

type NewVerifierFunc func(raw map[string]any) (Verifier, error)

type Registry struct {
	factories map[string]NewVerifierFunc
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]NewVerifierFunc{}}
}

func (r *Registry) Register(typeName string, fn NewVerifierFunc) {
	r.factories[typeName] = fn
}

func (r *Registry) Build(typeName string, raw map[string]any) (Verifier, error) {
	fn, ok := r.factories[typeName]
	if !ok {
		return nil, fmt.Errorf("captcha: unknown provider %q", typeName)
	}
	return fn(raw)
}
