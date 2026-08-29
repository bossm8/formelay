// Package config defines the YAML schema (global config.yaml + per-form
// forms.d/*.yaml), loads and validates it, and exposes a hot-reloadable
// Store. Types here are the raw, structural config — building runtime
// objects (notifiers, verifiers, classifiers, parsed templates) from them is
// the orchestration layer's job (see cmd/formelay), keeping this package
// free of dependencies on any concrete channel/captcha/classifier package.
package config

import "github.com/bossm8/formelay/internal/yamlutil"

type GlobalConfig struct {
	Server       ServerConfig       `yaml:"server"`
	FormsDir     string             `yaml:"forms_dir"`
	TemplatesDir string             `yaml:"templates_dir"`
	Security     SecurityConfig     `yaml:"security"`
	RateLimit    RateLimitConfig    `yaml:"rate_limit"`
	SMTPDefaults SMTPDefaultsConfig `yaml:"smtp_defaults"`
	Logging      LoggingConfig      `yaml:"logging"`
	Reload       ReloadConfig       `yaml:"reload"`
	Metrics      MetricsConfig      `yaml:"metrics"`
	Health       HealthConfig       `yaml:"health"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type ServerConfig struct {
	ListenAddr          string            `yaml:"listen_addr"`
	ReadTimeout         yamlutil.Duration `yaml:"read_timeout"`
	WriteTimeout        yamlutil.Duration `yaml:"write_timeout"`
	IdleTimeout         yamlutil.Duration `yaml:"idle_timeout"`
	ReadHeaderTimeout   yamlutil.Duration `yaml:"read_header_timeout"`
	ShutdownGracePeriod yamlutil.Duration `yaml:"shutdown_grace_period"`
	TLS                 TLSConfig         `yaml:"tls"`
	TrustedProxies      []string          `yaml:"trusted_proxies"`
}

type SecurityConfig struct {
	DefaultAllowedOrigins []string `yaml:"default_allowed_origins"`
	MaxBodyBytes          int64    `yaml:"max_body_bytes"`
}

type RateRule struct {
	Rate   float64           `yaml:"rate"`
	Window yamlutil.Duration `yaml:"window"`
	Burst  float64           `yaml:"burst"`
}

type RateLimitDefaults struct {
	PerIP   RateRule `yaml:"per_ip"`
	PerForm RateRule `yaml:"per_form"`
	Global  RateRule `yaml:"global"`
}

type ValkeyConfig struct {
	Addresses   []string          `yaml:"addresses"`
	PasswordEnv string            `yaml:"password_env"`
	DB          int               `yaml:"db"`
	DialTimeout yamlutil.Duration `yaml:"dial_timeout"`
	KeyPrefix   string            `yaml:"key_prefix"`
	OnError     string            `yaml:"on_error"` // allow | deny
}

type RateLimitConfig struct {
	Backend         string            `yaml:"backend"` // memory | valkey
	Default         RateLimitDefaults `yaml:"default"`
	CleanupInterval yamlutil.Duration `yaml:"cleanup_interval"`
	BucketIdleTTL   yamlutil.Duration `yaml:"bucket_idle_ttl"`
	Valkey          ValkeyConfig      `yaml:"valkey"`
}

type SMTPDefaultsConfig struct {
	Host        string            `yaml:"host"`
	Port        int               `yaml:"port"`
	Username    string            `yaml:"username"`
	PasswordEnv string            `yaml:"password_env"`
	StartTLS    bool              `yaml:"starttls"`
	From        string            `yaml:"from"`
	Timeout     yamlutil.Duration `yaml:"timeout"`
}

type AuditConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Format         string `yaml:"format"`
	LogFieldValues bool   `yaml:"log_field_values"`
}

type LoggingConfig struct {
	Level  string      `yaml:"level"`
	Format string      `yaml:"format"`
	Audit  AuditConfig `yaml:"audit"`
}

type ReloadConfig struct {
	WatchFiles   bool `yaml:"watch_files"`
	HandleSIGHUP bool `yaml:"handle_sighup"`
}

type MetricsConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"`
	Path       string `yaml:"path"`
}

type HealthConfig struct {
	LivenessPath  string `yaml:"liveness_path"`
	ReadinessPath string `yaml:"readiness_path"`
}

// FormConfig is one forms.d/<slug>.yaml file.
type FormConfig struct {
	ID               string             `yaml:"id"`
	DisplayName      string             `yaml:"display_name"`
	Enabled          *bool              `yaml:"enabled"`
	AllowedOrigins   []string           `yaml:"allowed_origins"`
	Auth             AuthConfig         `yaml:"auth"`
	Honeypot         HoneypotConfig     `yaml:"honeypot"`
	Captcha          CaptchaConfig      `yaml:"captcha"`
	SpamFilter       SpamFilterConfig   `yaml:"spam_filter"`
	RateLimit        *RateLimitOverride `yaml:"rate_limit"`
	Fields           FieldsConfig       `yaml:"fields"`
	Channels         []ChannelConfig    `yaml:"channels"`
	ChannelsRequired string             `yaml:"channels_required"` // any | all | none
}

func (f *FormConfig) IsEnabled() bool {
	return f.Enabled == nil || *f.Enabled
}

// AuthConfig identifies who may submit to a form. SiteKey is a public
// capability token (like a reCAPTCHA/Turnstile "site key"), not a secret:
// anything embedded in a browser-facing site can be read by anyone who
// opens dev tools. It scopes the form and can be rotated independently of
// AllowedOrigins, but provides no confidentiality — see README Security
// Model.
type AuthConfig struct {
	SiteKey       string `yaml:"site_key"`
	Transport     string `yaml:"transport"` // header | form_field
	HeaderName    string `yaml:"header_name"`
	FormFieldName string `yaml:"form_field_name"`
}

type HoneypotConfig struct {
	FieldName string `yaml:"field_name"`
}

type CaptchaConfig struct {
	Enabled         bool    `yaml:"enabled"`
	Provider        string  `yaml:"provider"`
	SecretEnv       string  `yaml:"secret_env"`
	ResponseField   string  `yaml:"response_field"`
	OnError         string  `yaml:"on_error"` // fail_open | fail_closed
	VerifyURL       string  `yaml:"verify_url"`
	RequestEncoding string  `yaml:"request_encoding"`
	SecretParam     string  `yaml:"secret_param"`
	ResponseParam   string  `yaml:"response_param"`
	RemoteIPParam   string  `yaml:"remoteip_param"`
	SuccessField    string  `yaml:"success_field"`
	ScoreField      string  `yaml:"score_field"`
	MinScore        float64 `yaml:"min_score"`
}

type SpamProviderConfig struct {
	Type      string            `yaml:"type"`
	APIBase   string            `yaml:"api_base"`
	APIKeyEnv string            `yaml:"api_key_env"`
	Model     string            `yaml:"model"`
	Timeout   yamlutil.Duration `yaml:"timeout"`
}

type SpamRouteConfig struct {
	SpamChannels  []string `yaml:"spam_channels"`
	ErrorChannels []string `yaml:"error_channels"`
	SpamTemplate  string   `yaml:"spam_template"`
	ErrorTemplate string   `yaml:"error_template"`
}

// SpamAction is the shared vocabulary for both on_spam and on_error.
type SpamAction string

const (
	SpamActionDeliver       SpamAction = "deliver"
	SpamActionDeliverTagged SpamAction = "deliver_tagged"
	SpamActionDrop          SpamAction = "drop"
	SpamActionRoute         SpamAction = "route"
)

type SpamFilterConfig struct {
	Enabled        bool               `yaml:"enabled"`
	Provider       SpamProviderConfig `yaml:"provider"`
	SystemTemplate string             `yaml:"system_template"`
	SystemInline   string             `yaml:"system_template_inline"`
	UserTemplate   string             `yaml:"user_template"`
	UserInline     string             `yaml:"user_template_inline"`
	OnSpam         SpamAction         `yaml:"on_spam"`
	OnError        SpamAction         `yaml:"on_error"`
	Route          SpamRouteConfig    `yaml:"route"`
}

type RateLimitOverride struct {
	PerIP   *RateRule `yaml:"per_ip"`
	PerForm *RateRule `yaml:"per_form"`
}

type FieldsConfig struct {
	Required       []string          `yaml:"required"`
	Validators     map[string]string `yaml:"validators"`
	MaxFieldLength int               `yaml:"max_field_length"`
}

type ChannelConfig struct {
	ID      string         `yaml:"id"`
	Type    string         `yaml:"type"`
	Enabled *bool          `yaml:"enabled"`
	Config  map[string]any `yaml:"config"`
}

func (c *ChannelConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}
