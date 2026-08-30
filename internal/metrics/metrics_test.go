package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestNew(t *testing.T) {
	m := New("1.2.3", "abc123", "go1.27")

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	var buildInfo *dto.MetricFamily
	for _, f := range families {
		if f.GetName() == "formelay_build_info" {
			buildInfo = f
			break
		}
	}
	if buildInfo == nil {
		t.Fatal("expected formelay_build_info to be registered and gathered")
	}
	if len(buildInfo.Metric) != 1 {
		t.Fatalf("expected exactly 1 formelay_build_info series, got %d", len(buildInfo.Metric))
	}
	metric := buildInfo.Metric[0]
	if metric.GetGauge().GetValue() != 1 {
		t.Fatalf("formelay_build_info value = %v, want 1", metric.GetGauge().GetValue())
	}
	labels := map[string]string{}
	for _, l := range metric.Label {
		labels[l.GetName()] = l.GetValue()
	}
	want := map[string]string{"version": "1.2.3", "commit": "abc123", "go_version": "go1.27"}
	for k, v := range want {
		if labels[k] != v {
			t.Fatalf("label %q = %q, want %q", k, labels[k], v)
		}
	}
}

// TestNewIsUsableTwiceIndependently proves each call builds its own
// dedicated registry (not the global default), so two Metrics instances
// (e.g. across tests, or a future multi-tenant use) never collide via
// duplicate registration.
func TestNewIsUsableTwiceIndependently(t *testing.T) {
	New("a", "b", "c")
	New("d", "e", "f") // must not panic (MustRegister) due to duplicate registration
}
