package config

import "time"

// Snapshot is one fully-loaded, structurally-validated configuration.
type Snapshot struct {
	Global *GlobalConfig
	Forms  map[string]*FormConfig
	Loaded time.Time
}
