package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGlobal(t *testing.T) {
	t.Run("valid file round-trips realistic values", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeFile(t, path, `
server:
  listen_addr: "0.0.0.0:9999"
  read_timeout: 5s
forms_dir: "/etc/formelay/forms"
templates_dir: "/etc/formelay/templates"
rate_limit:
  backend: memory
logging:
  level: debug
  format: text
`)
		g, err := LoadGlobal(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Server.ListenAddr != "0.0.0.0:9999" {
			t.Fatalf("ListenAddr = %q, want 0.0.0.0:9999", g.Server.ListenAddr)
		}
		if g.Server.ReadTimeout.Std().String() != "5s" {
			t.Fatalf("ReadTimeout = %v, want 5s", g.Server.ReadTimeout.Std())
		}
		if g.Logging.Level != "debug" || g.Logging.Format != "text" {
			t.Fatalf("Logging = %+v, want level=debug format=text", g.Logging)
		}
	})

	t.Run("unknown top-level key is a strict-decode error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeFile(t, path, "totally_bogus_key: 1\n")
		if _, err := LoadGlobal(path); err == nil {
			t.Fatal("expected strict-decode error for unknown top-level key")
		}
	})

	t.Run("missing file is an error", func(t *testing.T) {
		if _, err := LoadGlobal(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
			t.Fatal("expected error for a nonexistent config file")
		}
	})

	t.Run("empty-string duration is a decode error, unlike an omitted key", func(t *testing.T) {
		dir := t.TempDir()

		withEmpty := filepath.Join(dir, "empty.yaml")
		writeFile(t, withEmpty, `
server:
  listen_addr: "0.0.0.0:8080"
  read_timeout: ""
`)
		if _, err := LoadGlobal(withEmpty); err == nil {
			t.Fatal(`expected read_timeout: "" to be a decode error (time.ParseDuration("") fails)`)
		}

		omitted := filepath.Join(dir, "omitted.yaml")
		writeFile(t, omitted, `
server:
  listen_addr: "0.0.0.0:8080"
`)
		g, err := LoadGlobal(omitted)
		if err != nil {
			t.Fatalf("omitting read_timeout entirely must not error: %v", err)
		}
		if g.Server.ReadTimeout.Std() != 0 {
			t.Fatalf("ReadTimeout = %v, want zero value when omitted", g.Server.ReadTimeout.Std())
		}
	})

	t.Run("partial logging override leaves sibling defaults untouched", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeFile(t, path, `
server:
  listen_addr: "0.0.0.0:8080"
logging:
  level: debug
`)
		g, err := LoadGlobal(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if g.Logging.Level != "debug" {
			t.Fatalf("Logging.Level = %q, want debug", g.Logging.Level)
		}
		def := DefaultGlobalConfig()
		if g.Logging.Format != def.Logging.Format {
			t.Fatalf("Logging.Format = %q, want untouched default %q", g.Logging.Format, def.Logging.Format)
		}
		if g.Logging.Audit.Enabled != def.Logging.Audit.Enabled {
			t.Fatalf("Logging.Audit.Enabled = %v, want untouched default %v", g.Logging.Audit.Enabled, def.Logging.Audit.Enabled)
		}
	})
}

func TestLoadForms(t *testing.T) {
	t.Run("valid dir with multiple forms", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: a\nauth:\n  site_key: k\n")
		writeFile(t, filepath.Join(dir, "b.yaml"), "id: b\nauth:\n  site_key: k\n")
		forms, err := LoadForms(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(forms) != 2 || forms["a"] == nil || forms["b"] == nil {
			t.Fatalf("expected forms a and b, got %v", forms)
		}
	})

	t.Run("missing id is an error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "auth:\n  site_key: k\n")
		if _, err := LoadForms(dir); err == nil {
			t.Fatal("expected error for a form missing 'id'")
		}
	})

	t.Run("duplicate id across files is an error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: dup\nauth:\n  site_key: k\n")
		writeFile(t, filepath.Join(dir, "b.yaml"), "id: dup\nauth:\n  site_key: k\n")
		if _, err := LoadForms(dir); err == nil {
			t.Fatal("expected error for duplicate form id across files")
		}
	})

	t.Run("non-yaml files are ignored", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: a\nauth:\n  site_key: k\n")
		writeFile(t, filepath.Join(dir, "README.md"), "not a form")
		writeFile(t, filepath.Join(dir, "notes.txt"), "not a form either")
		forms, err := LoadForms(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(forms) != 1 {
			t.Fatalf("expected exactly 1 form (non-yaml files ignored), got %d: %v", len(forms), forms)
		}
	})

	t.Run("subdirectories are ignored, not recursed into", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: a\nauth:\n  site_key: k\n")
		writeFile(t, filepath.Join(dir, "nested", "b.yaml"), "id: b\nauth:\n  site_key: k\n")
		forms, err := LoadForms(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(forms) != 1 || forms["b"] != nil {
			t.Fatalf("expected only the top-level form, subdirectory must be ignored: %v", forms)
		}
	})

	t.Run("nonexistent dir is a wrapped error", func(t *testing.T) {
		if _, err := LoadForms(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
			t.Fatal("expected error for a nonexistent forms_dir")
		}
	})

	t.Run("channels_required defaults to any when omitted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: a\nauth:\n  site_key: k\n")
		forms, err := LoadForms(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if forms["a"].ChannelsRequired != "any" {
			t.Fatalf("ChannelsRequired = %q, want default %q", forms["a"].ChannelsRequired, "any")
		}
	})

	t.Run("max_field_length is not defaulted at this layer", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.yaml"), "id: a\nauth:\n  site_key: k\n")
		forms, err := LoadForms(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if forms["a"].Fields.MaxFieldLength != 0 {
			t.Fatalf("MaxFieldLength = %d, want 0: the documented 5000 default is applied outside internal/config", forms["a"].Fields.MaxFieldLength)
		}
	})
}
