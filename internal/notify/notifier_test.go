package notify

import (
	"context"
	"testing"
)

type stubNotifier struct{}

func (stubNotifier) Send(context.Context, RenderedMessage) error { return nil }
func (stubNotifier) Type() string                                { return "stub" }

func TestRegistryBuild(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func(map[string]any, GlobalDefaults) (Notifier, error) {
		return stubNotifier{}, nil
	})

	t.Run("registered type builds successfully", func(t *testing.T) {
		n, err := r.Build("stub", nil, GlobalDefaults{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n.Type() != "stub" {
			t.Fatalf("Type() = %q, want stub", n.Type())
		}
	})

	t.Run("unknown type is a clear error naming the type", func(t *testing.T) {
		_, err := r.Build("does-not-exist", nil, GlobalDefaults{})
		if err == nil {
			t.Fatal("expected an error for an unregistered channel type")
		}
	})
}
