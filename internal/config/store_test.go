package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	t.Run("a shared_key referencing an undefined outbound bucket fails Load", func(t *testing.T) {
		dir := t.TempDir()
		path := writeValidGlobal(t, dir)
		writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: k
channels:
  - id: ch1
    type: webhook
    rate_limit:
      shared_key: "not-defined-anywhere"
    config:
      url: "https://example.invalid/hook"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for a shared_key with no matching rate_limit.outbound_buckets entry")
		}
		if !strings.Contains(err.Error(), `shared_key "not-defined-anywhere"`) {
			t.Fatalf("expected error to name the missing shared_key, got: %v", err)
		}
	})

	t.Run("a valid shared_key reference resolves onto the block from the global bucket", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeFile(t, path, `
server:
  listen_addr: "0.0.0.0:8080"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
rate_limit:
  outbound_buckets:
    primary-smtp:
      rate: 10
      window: 1m
      burst: 10
      on_limit: fail
`)
		writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: k
channels:
  - id: ch1
    type: webhook
    rate_limit:
      shared_key: "primary-smtp"
    config:
      url: "https://example.invalid/hook"
`)
		snap, err := Load(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rl := snap.Forms["contact"].Channels[0].RateLimit
		if rl == nil {
			t.Fatal("expected ch1.rate_limit to be populated")
		}
		if rl.Rate != 10 || rl.Burst != 10 || rl.Window.Std() != time.Minute || rl.OnLimit != "fail" {
			t.Fatalf("expected shared_key to resolve to the bucket's numbers, got %+v", rl)
		}
	})

	t.Run("two blocks sharing one key resolve to identical numbers, not whichever one happened to run last", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		writeFile(t, path, `
server:
  listen_addr: "0.0.0.0:8080"
forms_dir: "`+filepath.Join(dir, "forms.d")+`"
templates_dir: "`+filepath.Join(dir, "templates")+`"
rate_limit:
  outbound_buckets:
    primary-smtp:
      rate: 7
      window: 1m
      burst: 3
      on_limit: wait
      max_wait: 2s
`)
		writeFile(t, filepath.Join(dir, "forms.d", "contact.yaml"), `
id: contact
auth:
  site_key: k
channels:
  - id: ch1
    type: webhook
    rate_limit:
      shared_key: "primary-smtp"
    config:
      url: "https://example.invalid/hook"
  - id: ch2
    type: webhook
    rate_limit:
      shared_key: "primary-smtp"
    config:
      url: "https://example.invalid/hook"
`)
		snap, err := Load(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rl1, rl2 := snap.Forms["contact"].Channels[0].RateLimit, snap.Forms["contact"].Channels[1].RateLimit
		if *rl1 != *rl2 {
			t.Fatalf("expected both blocks sharing one key to resolve identically, got ch1=%+v ch2=%+v", rl1, rl2)
		}
		if rl1.Rate != 7 || rl1.Burst != 3 || rl1.OnLimit != "wait" || rl1.MaxWait.Std() != 2*time.Second {
			t.Fatalf("unexpected resolved values: %+v", rl1)
		}
	})
}
