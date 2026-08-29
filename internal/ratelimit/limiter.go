// Package ratelimit defines a backend-agnostic rate-limit Store; the memory
// and valkey subpackages provide the two selectable implementations.
package ratelimit

import (
	"context"
	"time"
)

// Store checks and consumes one token bucket identified by key.
// Implementations must be safe for concurrent use.
type Store interface {
	// Allow reports whether a request against key is allowed under the
	// given rate (tokens/window), burst, and window, consuming a token if so.
	Allow(ctx context.Context, key string, rate, burst float64, window time.Duration) (bool, error)
}

// Rule is one rate-limit rule (per-IP, per-form, or global).
type Rule struct {
	Rate   float64
	Window time.Duration
	Burst  float64
}
