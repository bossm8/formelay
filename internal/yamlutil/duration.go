package yamlutil

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is time.Duration with YAML (un)marshaling from/to Go duration
// strings ("1m", "500ms") — gopkg.in/yaml.v3 has no built-in support for
// time.Duration, so every config struct with a duration field uses this
// type instead.
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}
