package render

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestResolveSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body.tmpl"), []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	absPath := filepath.Join(dir, "absolute.tmpl")
	if err := os.WriteFile(absPath, []byte("from absolute path"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("inline wins over path when both are given", func(t *testing.T) {
		src, err := ResolveSource(dir, "body.tmpl", "inline wins")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if src != "inline wins" {
			t.Fatalf("got %q, want inline source", src)
		}
	})

	t.Run("relative path joins with templatesDir", func(t *testing.T) {
		src, err := ResolveSource(dir, "body.tmpl", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if src != "from file" {
			t.Fatalf("got %q, want file contents", src)
		}
	})

	t.Run("absolute path is used as-is, ignoring templatesDir", func(t *testing.T) {
		src, err := ResolveSource("/some/unrelated/dir", absPath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if src != "from absolute path" {
			t.Fatalf("got %q, want absolute file contents", src)
		}
	})

	t.Run("neither path nor inline given is an error", func(t *testing.T) {
		if _, err := ResolveSource(dir, "", ""); err == nil {
			t.Fatal("expected an error when neither a path nor inline source is given")
		}
	})

	t.Run("nonexistent relative path is an error", func(t *testing.T) {
		if _, err := ResolveSource(dir, "does-not-exist.tmpl", ""); err == nil {
			t.Fatal("expected an error for a nonexistent template file")
		}
	})
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
