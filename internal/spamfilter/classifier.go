// Package spamfilter defines the pluggable spam-classification stage. The
// built-in `ai` implementation judges submitted content via an
// OpenAI-compatible chat-completions endpoint; see the ai subpackage.
package spamfilter

import (
	"context"
	"fmt"

	"github.com/bossm8/formelay/internal/render"
)

// Verdict is the outcome of classifying one submission.
type Verdict struct {
	IsSpam bool
	Reason string
}

// Classifier judges whether a submission is spam. A non-nil error (provider
// timeout/failure/malformed response) is handled by the caller via the
// form's `on_error` action, independently of `on_spam` for IsSpam=true.
type Classifier interface {
	Classify(ctx context.Context, data render.SubmissionData) (Verdict, error)
	Type() string
}

// PromptSource carries already-resolved (file-read or inline) prompt
// template source text for a form's spam filter. An empty field means the
// form didn't configure one, and the classifier implementation should fall
// back to its own built-in default (see ai.New).
type PromptSource struct {
	SystemSource string
	UserSource   string
}

// NewClassifierFunc builds a Classifier. Template resolution (file vs
// inline vs built-in default) happens in the orchestration layer, once per
// form per reload — prompts carries the result so this stays a pure
// function of already-resolved inputs.
type NewClassifierFunc func(raw map[string]any, prompts PromptSource) (Classifier, error)

type Registry struct {
	factories map[string]NewClassifierFunc
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]NewClassifierFunc{}}
}

func (r *Registry) Register(typeName string, fn NewClassifierFunc) {
	r.factories[typeName] = fn
}

func (r *Registry) Build(typeName string, raw map[string]any, prompts PromptSource) (Classifier, error) {
	fn, ok := r.factories[typeName]
	if !ok {
		return nil, fmt.Errorf("spamfilter: unknown provider type %q", typeName)
	}
	return fn(raw, prompts)
}
