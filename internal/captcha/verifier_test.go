package captcha

import (
	"context"
	"testing"
)

type stubVerifier struct{}

func (stubVerifier) Verify(context.Context, string, string) (bool, error) { return true, nil }

func TestRegistryBuild(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func(map[string]any) (Verifier, error) {
		return stubVerifier{}, nil
	})

	t.Run("registered provider builds successfully", func(t *testing.T) {
		if _, err := r.Build("stub", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown provider is a clear error naming the provider", func(t *testing.T) {
		if _, err := r.Build("does-not-exist", nil); err == nil {
			t.Fatal("expected an error for an unregistered captcha provider")
		}
	})
}
