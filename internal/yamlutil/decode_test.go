package yamlutil

import "testing"

type sampleConfig struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}

func TestDecode(t *testing.T) {
	t.Run("valid map decodes into a struct", func(t *testing.T) {
		var out sampleConfig
		if err := Decode(map[string]any{"name": "alice", "port": 8080}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Name != "alice" || out.Port != 8080 {
			t.Fatalf("got %+v", out)
		}
	})

	t.Run("unknown extra key is silently ignored (lenient, unlike config/load.go's strict decoding)", func(t *testing.T) {
		var out sampleConfig
		if err := Decode(map[string]any{"name": "alice", "bogus_extra_key": 1}, &out); err != nil {
			t.Fatalf("expected no error for an unknown key, got: %v", err)
		}
		if out.Name != "alice" {
			t.Fatalf("got %+v", out)
		}
	})

	t.Run("nil map decodes to the zero value", func(t *testing.T) {
		var out sampleConfig
		if err := Decode(nil, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Name != "" || out.Port != 0 {
			t.Fatalf("got %+v, want zero value", out)
		}
	})
}

func TestToMap(t *testing.T) {
	t.Run("struct round-trips to a map with expected keys", func(t *testing.T) {
		m, err := ToMap(sampleConfig{Name: "alice", Port: 8080})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m["name"] != "alice" {
			t.Fatalf("m[name] = %v, want alice", m["name"])
		}
		if m["port"] != 8080 {
			t.Fatalf("m[port] = %v, want 8080", m["port"])
		}
	})

	t.Run("ToMap then Decode round-trips back to the original struct", func(t *testing.T) {
		orig := sampleConfig{Name: "bob", Port: 9090}
		m, err := ToMap(orig)
		if err != nil {
			t.Fatalf("ToMap: %v", err)
		}
		var out sampleConfig
		if err := Decode(m, &out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if out != orig {
			t.Fatalf("round-trip mismatch: got %+v, want %+v", out, orig)
		}
	})
}
