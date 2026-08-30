package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bossm8/formelay/internal/yamlutil"
)

// Config is decoded from a form's `captcha:` block. Unset fields are seeded
// from a named preset (see presets.go) before the operator's own YAML
// overrides are applied.
type Config struct {
	SecretEnv       string            `yaml:"secret_env"`
	VerifyURL       string            `yaml:"verify_url"`
	RequestEncoding string            `yaml:"request_encoding"` // form | json
	SecretParam     string            `yaml:"secret_param"`
	ResponseParam   string            `yaml:"response_param"`
	RemoteIPParam   string            `yaml:"remoteip_param"`
	SuccessField    string            `yaml:"success_field"`
	ScoreField      string            `yaml:"score_field"`
	MinScore        float64           `yaml:"min_score"`
	Timeout         yamlutil.Duration `yaml:"timeout"`
}

func (c *Config) Validate() error {
	if c.VerifyURL == "" {
		return errors.New("captcha: 'verify_url' is required (set 'provider' to a known preset, or set verify_url explicitly for provider: generic)")
	}
	u, err := url.Parse(c.VerifyURL)
	if err != nil || u.Scheme != "https" {
		return errors.New("captcha: 'verify_url' must be a valid https URL")
	}
	if c.SecretEnv == "" {
		return errors.New("captcha: 'secret_env' is required")
	}
	if os.Getenv(c.SecretEnv) == "" {
		return fmt.Errorf("captcha: environment variable %q referenced by 'secret_env' is not set", c.SecretEnv)
	}
	if c.RequestEncoding != "form" && c.RequestEncoding != "json" {
		return errors.New("captcha: 'request_encoding' must be 'form' or 'json'")
	}
	if c.SuccessField == "" {
		return errors.New("captcha: 'success_field' is required")
	}
	return nil
}

type genericVerifier struct {
	cfg    Config
	client *http.Client
}

// NewFactory returns a NewVerifierFunc pre-seeded with presetName's defaults
// ("" for no preset, i.e. `provider: generic`).
func NewFactory(presetName string) NewVerifierFunc {
	return func(raw map[string]any) (Verifier, error) {
		cfg := Config{Timeout: yamlutil.Duration(5 * time.Second)}
		if err := yamlutil.Decode(raw, &cfg); err != nil {
			return nil, fmt.Errorf("captcha: decode config: %w", err)
		}
		// Preset defaults are applied *after* decoding the operator's own
		// config, filling in only fields still at their zero value — raw
		// is derived from the full config.CaptchaConfig struct, so it
		// always carries every field (including unset ones as ""), and
		// decoding first would let those clobber the preset.
		if p, ok := presets[presetName]; ok {
			if cfg.VerifyURL == "" {
				cfg.VerifyURL = p.VerifyURL
			}
			if cfg.RequestEncoding == "" {
				cfg.RequestEncoding = p.RequestEncoding
			}
			if cfg.SecretParam == "" {
				cfg.SecretParam = p.SecretParam
			}
			if cfg.ResponseParam == "" {
				cfg.ResponseParam = p.ResponseParam
			}
			if cfg.RemoteIPParam == "" {
				cfg.RemoteIPParam = p.RemoteIPParam
			}
			if cfg.SuccessField == "" {
				cfg.SuccessField = p.SuccessField
			}
			if cfg.ScoreField == "" {
				cfg.ScoreField = p.ScoreField
			}
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		return &genericVerifier{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout.Std()}}, nil
	}
}

func (v *genericVerifier) Verify(ctx context.Context, responseToken, remoteIP string) (bool, error) {
	secret := os.Getenv(v.cfg.SecretEnv)
	params := map[string]string{
		v.cfg.SecretParam:   secret,
		v.cfg.ResponseParam: responseToken,
	}
	if v.cfg.RemoteIPParam != "" && remoteIP != "" {
		params[v.cfg.RemoteIPParam] = remoteIP
	}

	var req *http.Request
	var err error
	if v.cfg.RequestEncoding == "json" {
		b, mErr := json.Marshal(params)
		if mErr != nil {
			return false, fmt.Errorf("captcha: encode request: %w", mErr)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.VerifyURL, strings.NewReader(string(b)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		form := url.Values{}
		for k, val := range params {
			form.Set(k, val)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.VerifyURL, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return false, fmt.Errorf("captcha: build request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("captcha: verify request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("captcha: read verify response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return false, fmt.Errorf("captcha: parse verify response: %w", err)
	}

	success, _ := result[v.cfg.SuccessField].(bool)
	if !success {
		return false, nil
	}
	if v.cfg.ScoreField != "" && v.cfg.MinScore > 0 {
		score, ok := result[v.cfg.ScoreField].(float64)
		if !ok || score < v.cfg.MinScore {
			return false, nil
		}
	}
	return true, nil
}
