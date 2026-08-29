package render

import (
	"encoding/json"
	texttemplate "text/template"
)

// FuncMap is shared by every template kind. `json` is the primary escaping
// tool for text/template targets (Discord, webhook, AI prompts), which have
// no context-aware auto-escaping the way html/template does.
var FuncMap = texttemplate.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
	"default": func(def, val string) string {
		if val == "" {
			return def
		}
		return val
	},
	"truncate": func(n int, s string) string {
		r := []rune(s)
		if len(r) <= n {
			return s
		}
		return string(r[:n])
	},
}
