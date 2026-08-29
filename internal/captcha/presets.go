package captcha

// preset holds the default field/URL shape for a named provider. Turnstile,
// hCaptcha, and reCAPTCHA v2/v3 all speak the same reCAPTCHA-derived
// protocol, so these are just default values for the one generic
// implementation in generic.go — not separate Go implementations.
type preset struct {
	VerifyURL       string
	RequestEncoding string
	SecretParam     string
	ResponseParam   string
	RemoteIPParam   string
	SuccessField    string
	ScoreField      string
}

var presets = map[string]preset{
	"turnstile": {
		VerifyURL:       "https://challenges.cloudflare.com/turnstile/v0/siteverify",
		RequestEncoding: "form",
		SecretParam:     "secret",
		ResponseParam:   "response",
		RemoteIPParam:   "remoteip",
		SuccessField:    "success",
	},
	"hcaptcha": {
		VerifyURL:       "https://api.hcaptcha.com/siteverify",
		RequestEncoding: "form",
		SecretParam:     "secret",
		ResponseParam:   "response",
		RemoteIPParam:   "remoteip",
		SuccessField:    "success",
	},
	"recaptcha_v2": {
		VerifyURL:       "https://www.google.com/recaptcha/api/siteverify",
		RequestEncoding: "form",
		SecretParam:     "secret",
		ResponseParam:   "response",
		RemoteIPParam:   "remoteip",
		SuccessField:    "success",
	},
	"recaptcha_v3": {
		VerifyURL:       "https://www.google.com/recaptcha/api/siteverify",
		RequestEncoding: "form",
		SecretParam:     "secret",
		ResponseParam:   "response",
		RemoteIPParam:   "remoteip",
		SuccessField:    "success",
		ScoreField:      "score",
	},
}
