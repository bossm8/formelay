// Package render parses and executes per-form, per-channel templates.
package render

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	texttemplate "text/template"
)

// Kind selects the escaping context a template is rendered into.
type Kind int

const (
	// KindText has no context-aware escaping; callers must escape
	// explicitly (e.g. via the `json` FuncMap entry) for JSON/plaintext
	// targets such as Discord, generic webhooks, and AI prompts.
	KindText Kind = iota
	// KindHTML auto-escapes interpolated values for safe HTML output,
	// used for the email body.
	KindHTML
)

// Template is a parsed, ready-to-execute template of either kind.
type Template struct {
	kind Kind
	text *texttemplate.Template
	html *htmltemplate.Template
}

// Parse parses template source text. name is used only for error messages.
func Parse(kind Kind, name, source string) (*Template, error) {
	switch kind {
	case KindHTML:
		// missingkey=zero: a submitted field that's absent from .Fields
		// (e.g. optional field never filled in) renders as an empty string
		// rather than failing template execution outright.
		t, err := htmltemplate.New(name).Funcs(FuncMap).Option("missingkey=zero").Parse(source)
		if err != nil {
			return nil, fmt.Errorf("render: parse html template %q: %w", name, err)
		}
		return &Template{kind: kind, html: t}, nil
	default:
		t, err := texttemplate.New(name).Funcs(FuncMap).Option("missingkey=zero").Parse(source)
		if err != nil {
			return nil, fmt.Errorf("render: parse text template %q: %w", name, err)
		}
		return &Template{kind: kind, text: t}, nil
	}
}

// Execute renders the template against submission data.
func (t *Template) Execute(data SubmissionData) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if t.kind == KindHTML {
		err = t.html.Execute(&buf, data)
	} else {
		err = t.text.Execute(&buf, data)
	}
	if err != nil {
		return nil, fmt.Errorf("render: execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// ResolveSource returns the template source text for a channel/form template
// reference: `inline` wins if non-empty, otherwise `ref` is read as a file
// path relative to templatesDir (or absolute).
func ResolveSource(templatesDir, ref, inline string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if ref == "" {
		return "", fmt.Errorf("render: neither a template path nor inline template given")
	}
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(templatesDir, ref)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("render: read template %q: %w", ref, err)
	}
	return string(b), nil
}
