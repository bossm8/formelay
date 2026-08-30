package yamlutil

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDurationUnmarshalYAML(t *testing.T) {
	t.Run("valid duration string", func(t *testing.T) {
		var d Duration
		if err := yaml.Unmarshal([]byte(`"5s"`), &d); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Std() != 5*time.Second {
			t.Fatalf("got %v, want 5s", d.Std())
		}
	})

	t.Run("malformed duration string (no unit) is an error", func(t *testing.T) {
		var d Duration
		if err := yaml.Unmarshal([]byte(`"5"`), &d); err == nil {
			t.Fatal("expected an error for a duration string with no unit")
		}
	})

	t.Run("negative duration string currently succeeds (documented, not validated)", func(t *testing.T) {
		var d Duration
		if err := yaml.Unmarshal([]byte(`"-5s"`), &d); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Std() != -5*time.Second {
			t.Fatalf("got %v, want -5s", d.Std())
		}
	})

	t.Run("empty string is an error", func(t *testing.T) {
		var d Duration
		if err := yaml.Unmarshal([]byte(`""`), &d); err == nil {
			t.Fatal("expected an error for an empty duration string")
		}
	})
}

func TestDurationMarshalYAML(t *testing.T) {
	d := Duration(90 * time.Second)
	out, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var back Duration
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("unexpected error round-tripping: %v", err)
	}
	if back.Std() != d.Std() {
		t.Fatalf("round-trip mismatch: got %v, want %v", back.Std(), d.Std())
	}
}

func TestDurationStd(t *testing.T) {
	d := Duration(3 * time.Minute)
	if d.Std() != 3*time.Minute {
		t.Fatalf("Std() = %v, want 3m", d.Std())
	}
}
