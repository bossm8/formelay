package render

import (
	"encoding/json"
	"testing"
)

func TestHTMLTemplateEscapesFields(t *testing.T) {
	tmpl, err := Parse(KindHTML, "t", "<p>{{ .Fields.name }}</p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := SubmissionData{Fields: map[string]string{"name": "<script>alert(1)</script>"}}
	out, err := tmpl.Execute(data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := string(out)
	if got == "<p><script>alert(1)</script></p>" {
		t.Fatalf("expected HTML escaping, got raw script tag: %s", got)
	}
	want := "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestJSONFuncEscapesForTextTemplate(t *testing.T) {
	tmpl, err := Parse(KindText, "t", `{"name": {{ .Fields.name | json }}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := SubmissionData{Fields: map[string]string{"name": `"; DROP TABLE users; --` + "\nnewline"}}
	out, err := tmpl.Execute(data)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Must still be valid JSON after substitution.
	var parsed map[string]string
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("rendered output is not valid JSON: %v\noutput: %s", err, out)
	}
	if parsed["name"] != data.Fields["name"] {
		t.Fatalf("round-tripped value mismatch: got %q want %q", parsed["name"], data.Fields["name"])
	}
}

func TestDefaultFunc(t *testing.T) {
	tmpl, err := Parse(KindText, "t", `{{ default "anonymous" .Fields.name }}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := tmpl.Execute(SubmissionData{Fields: map[string]string{}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if string(out) != "anonymous" {
		t.Fatalf("got %q, want %q", out, "anonymous")
	}
}
