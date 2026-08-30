package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func writeValidGlobal(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `
server:
  listen_addr: "0.0.0.0:8080"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
`)
	return path
}

func TestLoad(t *testing.T) {
	t.Run("happy path populates a snapshot", func(t *testing.T) {
		dir := t.TempDir()
		path := writeValidGlobal(t, dir)
		writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), "id: contact\nauth:\n  site_key: k\n")

		snap, err := Load(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if snap.Global == nil {
			t.Fatal("expected Global to be populated")
		}
		if snap.Loaded.IsZero() {
			t.Fatal("expected Loaded to be set")
		}
		if _, ok := snap.Forms["contact"]; !ok {
			t.Fatalf("expected form 'contact' to be loaded, got %v", snap.Forms)
		}
	})

	t.Run("global validation failure is wrapped", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		// listen_addr empty -> ValidateGlobal error.
		writeFile(t, path, `
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
server:
  listen_addr: ""
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid global config")
		}
		if !strings.Contains(err.Error(), "validate global") {
			t.Fatalf("expected error to be wrapped with 'validate global' context, got: %v", err)
		}
	})

	t.Run("forms load failure is wrapped", func(t *testing.T) {
		dir := t.TempDir()
		path := writeValidGlobal(t, dir)
		// forms_dir points at DefaultGlobalConfig-overridden path that we
		// never create, so LoadForms's os.ReadDir fails.
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error when forms_dir does not exist")
		}
		if !strings.Contains(err.Error(), "load forms") {
			t.Fatalf("expected error to be wrapped with 'load forms' context, got: %v", err)
		}
	})

	t.Run("a single invalid form's validation failure is wrapped with its id", func(t *testing.T) {
		dir := t.TempDir()
		path := writeValidGlobal(t, dir)
		// Missing auth.site_key -> ValidateForm error.
		writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), "id: contact\n")

		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for a form failing validation")
		}
		if !strings.Contains(err.Error(), `validate form "contact"`) {
			t.Fatalf("expected error to name the failing form id, got: %v", err)
		}
	})
}
