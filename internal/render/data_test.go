package render

import (
	"reflect"
	"testing"
)

func TestWithFieldsLimitedTo(t *testing.T) {
	full := SubmissionData{
		Form:        FormMeta{ID: "contact", DisplayName: "Contact"},
		Fields:      map[string]string{"name": "Alice", "email": "alice@example.com", "message": "hi"},
		FieldsMulti: map[string][]string{"name": {"Alice"}, "email": {"alice@example.com"}, "message": {"hi"}},
	}

	t.Run("restricts to the allowlist", func(t *testing.T) {
		got := full.WithFieldsLimitedTo([]string{"message"})
		want := map[string]string{"message": "hi"}
		if !reflect.DeepEqual(got.Fields, want) {
			t.Fatalf("Fields = %v, want %v", got.Fields, want)
		}
		wantMulti := map[string][]string{"message": {"hi"}}
		if !reflect.DeepEqual(got.FieldsMulti, wantMulti) {
			t.Fatalf("FieldsMulti = %v, want %v", got.FieldsMulti, wantMulti)
		}
		// PII fields must be genuinely gone, not just empty-valued.
		if _, ok := got.Fields["name"]; ok {
			t.Fatalf("expected 'name' to be absent, not just empty")
		}
		if _, ok := got.Fields["email"]; ok {
			t.Fatalf("expected 'email' to be absent, not just empty")
		}
	})

	t.Run("leaves Form and Meta untouched", func(t *testing.T) {
		got := full.WithFieldsLimitedTo([]string{"message"})
		if got.Form != full.Form {
			t.Fatalf("Form changed: got %v, want %v", got.Form, full.Form)
		}
	})

	t.Run("empty allowlist yields zero fields, privacy-safe by default", func(t *testing.T) {
		got := full.WithFieldsLimitedTo(nil)
		if len(got.Fields) != 0 {
			t.Fatalf("expected no Fields, got %v", got.Fields)
		}
		if len(got.FieldsMulti) != 0 {
			t.Fatalf("expected no FieldsMulti, got %v", got.FieldsMulti)
		}
		if got.Form != full.Form {
			t.Fatalf("Form changed: got %v, want %v", got.Form, full.Form)
		}
	})

	t.Run("a listed but absent field is silently skipped", func(t *testing.T) {
		got := full.WithFieldsLimitedTo([]string{"message", "phone"})
		want := map[string]string{"message": "hi"}
		if !reflect.DeepEqual(got.Fields, want) {
			t.Fatalf("Fields = %v, want %v", got.Fields, want)
		}
	})

	t.Run("original is not mutated", func(t *testing.T) {
		_ = full.WithFieldsLimitedTo([]string{"message"})
		if len(full.Fields) != 3 {
			t.Fatalf("original Fields was mutated: %v", full.Fields)
		}
	})
}
