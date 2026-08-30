package honeypot

import "testing"

func TestTriggered(t *testing.T) {
	cases := []struct {
		name      string
		fields    map[string]string
		fieldName string
		want      bool
	}{
		{"empty field name never triggers, even if the field happens to be present", map[string]string{"website": "spam"}, "", false},
		{"present and non-empty triggers", map[string]string{"website": "spam"}, "website", true},
		{"present but empty does not trigger", map[string]string{"website": ""}, "website", false},
		{"absent does not trigger", map[string]string{"name": "Alice"}, "website", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Triggered(c.fields, c.fieldName); got != c.want {
				t.Fatalf("Triggered(%v, %q) = %v, want %v", c.fields, c.fieldName, got, c.want)
			}
		})
	}
}
