package config

import (
	"fmt"
	"time"
)

// Load parses and structurally validates config.yaml and its forms_dir into
// one Snapshot. It performs no atomic swap — the caller (internal/app) is
// responsible for also building runtime objects (notifiers, verifiers,
// classifiers, parsed templates) from the result and only then atomically
// publishing both together, so a config.Store reader can never observe a
// Snapshot whose registry-dependent parts (e.g. an unknown channel type)
// failed to build.
func Load(globalPath string) (*Snapshot, error) {
	global, err := LoadGlobal(globalPath)
	if err != nil {
		return nil, fmt.Errorf("config: load global: %w", err)
	}
	if err := ValidateGlobal(global); err != nil {
		return nil, fmt.Errorf("config: validate global: %w", err)
	}

	forms, err := LoadForms(global.FormsDir)
	if err != nil {
		return nil, fmt.Errorf("config: load forms: %w", err)
	}
	for id, f := range forms {
		if err := ValidateForm(f); err != nil {
			return nil, fmt.Errorf("config: validate form %q: %w", id, err)
		}
	}
	if err := resolveOutboundRateLimits(global, forms); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return &Snapshot{Global: global, Forms: forms, Loaded: time.Now()}, nil
}
