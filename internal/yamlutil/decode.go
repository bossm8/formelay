// Package yamlutil provides a small helper used by channel/captcha/classifier
// implementations to decode a generic map[string]any (as produced by the
// config loader for opaque per-type config blocks) into their own typed
// config struct, without pulling in a reflection-based mapping library.
package yamlutil

import "gopkg.in/yaml.v3"

// Decode round-trips raw through YAML marshal/unmarshal into out. It is only
// ever called at config-load/reload time, not per-request, so the extra
// encode/decode pass costs nothing that matters.
func Decode(raw map[string]any, out any) error {
	b, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, out)
}

// ToMap round-trips a typed config struct through YAML marshal/unmarshal
// into a generic map[string]any, for handing to a Registry.Build call that
// expects the same opaque raw-config shape the YAML config loader produces
// for per-type config blocks.
func ToMap(v any) (map[string]any, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
