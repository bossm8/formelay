package spamfilter

import (
	"context"
	"testing"

	"github.com/bossm8/formelay/internal/render"
)

type stubClassifier struct{}

func (stubClassifier) Classify(context.Context, render.SubmissionData) (Verdict, error) {
	return Verdict{}, nil
}

func TestRegistryBuild(t *testing.T) {
	r := NewRegistry()
	r.Register("stub", func(map[string]any, PromptSource) (Classifier, error) {
		return stubClassifier{}, nil
	})

	t.Run("registered provider type builds successfully", func(t *testing.T) {
		if _, err := r.Build("stub", nil, PromptSource{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown provider type is a clear error naming the type", func(t *testing.T) {
		if _, err := r.Build("does-not-exist", nil, PromptSource{}); err == nil {
			t.Fatal("expected an error for an unregistered spam-filter provider type")
		}
	})
}
