package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

func decodeStrict(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// LoadGlobal parses config.yaml.
func LoadGlobal(path string) (*GlobalConfig, error) {
	cfg := DefaultGlobalConfig()
	if err := decodeStrict(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadForms parses every *.yaml file in dir into a FormConfig, keyed by ID.
func LoadForms(dir string) (map[string]*FormConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read forms_dir %q: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)

	forms := map[string]*FormConfig{}
	for _, path := range files {
		fc := &FormConfig{ChannelsRequired: "any"}
		if err := decodeStrict(path, fc); err != nil {
			return nil, err
		}
		if fc.ID == "" {
			return nil, fmt.Errorf("%s: 'id' is required", path)
		}
		if _, dup := forms[fc.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate form id %q", path, fc.ID)
		}
		forms[fc.ID] = fc
	}
	return forms, nil
}

// DefaultGlobalConfig returns a GlobalConfig with sensible defaults, applied
// before the operator's config.yaml is decoded on top of it.
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Server: ServerConfig{
			ListenAddr: "0.0.0.0:8080",
		},
		FormsDir:     "/etc/formelay/forms",
		TemplatesDir: "/etc/formelay/templates",
		Security: SecurityConfig{
			MaxBodyBytes: 256 * 1024,
		},
		RateLimit: RateLimitConfig{
			Backend: "memory",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Audit:  AuditConfig{Enabled: true},
		},
		Reload:   ReloadConfig{WatchFiles: true, HandleSIGHUP: true, HandleHTTP: true, HTTPPath: "/reload"},
		Internal: InternalConfig{ListenAddr: "0.0.0.0:9696"},
		Metrics: MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
		Health: HealthConfig{
			LivenessPath:  "/healthz",
			ReadinessPath: "/readyz",
		},
	}
}
