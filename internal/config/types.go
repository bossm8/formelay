// Package config defines the YAML schema (global config.yaml + per-form
// forms/*.yaml), loads and validates it, and exposes a hot-reloadable
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
	MaxBodyBytes int64 `yaml:"max_body_bytes"`
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
	// OutboundBuckets names shared outbound rate-limit buckets — the only
	// place a shared bucket's actual rate/burst/window/on_limit/max_wait
	// live. A channel's or spam_filter's rate_limit block references one
	// of these by name via shared_key instead of defining its own
	// numbers (see OutboundRateLimitConfig) — mutually exclusive with
	// inline numbers, so every user of the same shared bucket is
	// guaranteed to agree on its capacity instead of racing to redefine
	// it differently.
	OutboundBuckets map[string]OutboundBucketConfig `yaml:"outbound_buckets"`
}

// OutboundBucketConfig is one named, shared outbound rate-limit bucket —
// see RateLimitConfig.OutboundBuckets.
type OutboundBucketConfig struct {
	Rate    float64           `yaml:"rate"`
	Window  yamlutil.Duration `yaml:"window"`
	Burst   float64           `yaml:"burst"`
	OnLimit string            `yaml:"on_limit"`
	MaxWait yamlutil.Duration `yaml:"max_wait"`
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

// AuditConfig has no separate Format field: the audit log is always
// structured JSON (that's the point, machine-parseable) regardless of
// LoggingConfig.Format, which only affects the general application log.
type AuditConfig struct {
	Enabled        bool `yaml:"enabled"`
	LogFieldValues bool `yaml:"log_field_values"`
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

// FormConfig is one forms/<slug>.yaml file.
type FormConfig struct {
	ID             string             `yaml:"id"`
	DisplayName    string             `yaml:"display_name"`
	Enabled        *bool              `yaml:"enabled"`
	AllowedOrigins []string           `yaml:"allowed_origins"`
	Auth           AuthConfig         `yaml:"auth"`
	Honeypot       HoneypotConfig     `yaml:"honeypot"`
	Captcha        CaptchaConfig      `yaml:"captcha"`
	SpamFilter     SpamFilterConfig   `yaml:"spam_filter"`
	RateLimit      *RateLimitOverride `yaml:"rate_limit"`
	Fields         FieldsConfig       `yaml:"fields"`
	Channels       []ChannelConfig    `yaml:"channels"`
	// ChannelsRequired only affects the HTTP response in response_mode:
	// sync (the default) — in async mode the response is already sent
	// before dispatch runs, so this still governs the eventual audit/
	// metrics outcome but can no longer change what the client saw.
	ChannelsRequired string `yaml:"channels_required"` // any | all | none
	// ResponseMode controls whether the HTTP response waits for the AI
	// spam filter + delivery to actually finish ("sync", the default) or
	// returns immediately after CAPTCHA passes, running both in the
	// background ("async"). CAPTCHA itself is always synchronous in
	// either mode.
	ResponseMode string `yaml:"response_mode"` // sync | async
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
	// IncludeFields is an allowlist of which submitted fields are sent to
	// the classifier — not just a template-level omission: a field left
	// off the list is dropped before Classify ever sees it, so a custom
	// user_template can't accidentally leak it back in. Privacy-safe by
	// default: empty/unset sends zero fields, not every field — an
	// operator must explicitly opt each field in.
	IncludeFields []string        `yaml:"include_fields"`
	OnSpam        SpamAction      `yaml:"on_spam"`
	OnError       SpamAction      `yaml:"on_error"`
	Route         SpamRouteConfig `yaml:"route"`
	// RateLimit throttles calls to the classifier — the same reusable
	// block as a channel's outbound rate_limit (see
	// OutboundRateLimitConfig), since an AI provider call is exactly the
	// same kind of rate-limited/cost-bearing third-party call a delivery
	// channel makes. Nil means no limiting. When the limit is exceeded (or
	// on_limit: wait times out), it's resolved exactly like any other
	// classifier failure — via OnError — not a separate action, so an
	// operator who already decided what "the classifier is unavailable"
	// means for their form doesn't need to decide it twice.
	RateLimit *OutboundRateLimitConfig `yaml:"rate_limit"`
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
	ID        string                   `yaml:"id"`
	Type      string                   `yaml:"type"`
	Enabled   *bool                    `yaml:"enabled"`
	RateLimit *OutboundRateLimitConfig `yaml:"rate_limit"`
	Config    map[string]any           `yaml:"config"`
}

// OutboundRateLimitConfig throttles an outbound, third-party call — a
// channel's delivery (e.g. to stay under a mail provider's sending quota)
// or the AI spam filter's classifier call — independent of the inbound
// rate_limit block above. Nil means no outbound limiting. The same block
// shape is reused for both, since they're the same kind of thing: a
// rate-limited/cost-bearing call to something formelay doesn't control.
//
// The two ways to fill this in are mutually exclusive, validated at
// config load (see validateOutboundRateLimit):
//   - SharedKey alone: draws from a bucket named in the global
//     rate_limit.outbound_buckets map, shared with every other channel
//     or spam filter (in any form) referencing the same name — for
//     multiple outbound calls that actually hit one real-world quota
//     (e.g. two email channels through the same SMTP account, or two
//     forms sharing one AI provider account). Rate/Window/Burst/OnLimit/
//     MaxWait are resolved from that bucket definition (see
//     resolveOutboundRateLimits), not set here directly, so every user
//     of the same shared_key is guaranteed to agree on its numbers.
//   - Rate/Window/Burst (+ optional OnLimit/MaxWait) alone: this
//     channel/spam filter gets its own private bucket, scoped by form
//     (+ channel id, for a channel).
type OutboundRateLimitConfig struct {
	Rate   float64           `yaml:"rate"`
	Window yamlutil.Duration `yaml:"window"`
	Burst  float64           `yaml:"burst"`
	// SharedKey names an entry in the global rate_limit.outbound_buckets
	// map (see the type doc comment above) — mutually exclusive with
	// Rate/Window/Burst/OnLimit/MaxWait.
	SharedKey string `yaml:"shared_key"`
	// OnLimit is "wait" (default) or "fail": wait briefly for a token
	// before proceeding, or fail immediately.
	OnLimit string `yaml:"on_limit"`
	// MaxWait bounds how long "wait" blocks before giving up as a failure.
	// Only meaningful when OnLimit is "wait" (the default). Default 5s.
	MaxWait yamlutil.Duration `yaml:"max_wait"`
}

func (c *ChannelConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}
