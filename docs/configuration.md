# Configuration reference

formelay reads two kinds of YAML: one global `config.yaml`, and one file per form under `forms_dir` (`forms.d/*.yaml` by default). Both are strictly decoded — an unknown key is a config-load error, not a silently ignored typo. Both are hot-reloaded (fsnotify watching the directories, or `SIGHUP`): a new config is fully validated, including parsing every template it references, before it replaces the running one. An invalid change is rejected and logged; the previous config keeps serving.

Working examples of everything below live in [`config.example/`](../config.example/) and are explained end-to-end in [examples.md](examples.md).

## `config.yaml` (global)

### `server`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `listen_addr` | string | `0.0.0.0:8080` | Public submission API listener. |
| `read_timeout`, `write_timeout`, `idle_timeout`, `read_header_timeout` | duration | `read_header_timeout` falls back to `5s` if unset; others `0` (no timeout) | Standard `net/http.Server` timeouts. |
| `shutdown_grace_period` | duration | `15s` | How long graceful shutdown waits for in-flight requests. |
| `tls.enabled`, `tls.cert_file`, `tls.key_file` | bool, string, string | — | If enabled, the public submission listener terminates TLS itself (`cert_file`/`key_file` are required and must exist, checked at config load). Most deployments instead put a reverse proxy in front and leave this off; it's here for the simple case of no proxy in front at all. Only the public listener is affected, the internal health/metrics listener always stays plain HTTP. |
| `trusted_proxies` | []string | `[]` | CIDRs (or bare IPs, treated as `/32`/`/128`) allowed to set `X-Forwarded-For`. Only trust your actual reverse proxy's address here. |

### `forms_dir`, `templates_dir`

Paths (strings) to the per-form YAML directory and the directory template `path:` references resolve against. Defaults: `/etc/formelay/forms.d`, `/etc/formelay/templates` (matching the Docker image's expected mount points).

### `security`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `max_body_bytes` | int | `262144` (256 KiB) | Hard cap on a submission's body size. |

### `rate_limit`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `backend` | `memory` \| `valkey` | `memory` | See [Rate limiting](#rate-limiting) below. |
| `default.per_ip`, `default.per_form`, `default.global` | rate rule | — | See [Rate rules](#rate-rules). Applied unless a form overrides `per_ip`/`per_form`. |
| `cleanup_interval`, `bucket_idle_ttl` | duration | `5m`, `10m` | Memory backend only: how often the janitor runs, and how long an idle bucket survives. |
| `valkey.addresses` | []string | — | Required when `backend: valkey`. |
| `valkey.password_env` | string | — | Env var holding the Valkey password (empty = no auth). |
| `valkey.db` | int | `0` | Valkey `SELECT` database index. |
| `valkey.dial_timeout` | duration | client default | Connection timeout. |
| `valkey.key_prefix` | string | `""` | Prefixed onto every rate-limit key, useful if multiple services share one Valkey. |
| `valkey.on_error` | `allow` \| `deny` | `allow` | What happens to a request if Valkey itself is unreachable *after* startup (see [Rate limiting](#rate-limiting)). |

#### Rate rules

A rate rule is `{rate: <float>, window: <duration>, burst: <float>}` — a token bucket refilling at `rate` tokens per `window`, holding at most `burst` tokens. Example: `{rate: 5, window: 1m, burst: 5}` allows a burst of 5 immediately, then steady-state 5/minute.

#### Rate limiting

- **`memory`**: in-process, sharded token buckets. Correct for a single running instance; state is lost on restart and isn't shared across replicas.
- **`valkey`**: bucket state lives in Valkey, updated atomically via a Lua script, so multiple formelay replicas share the same limits. `New()` connects eagerly at startup — an unreachable Valkey at boot is a hard startup failure. `on_error` only governs a *later* outage (a `Do()` call failing after a successful connection): `allow` (default) degrades to unrate-limited rather than rejecting all traffic; `deny` fails closed.

### `smtp_defaults`

Inherited by any `email` channel that doesn't override the same field itself.

| Field | Type | Meaning |
|---|---|---|
| `host`, `port` | string, int | SMTP server. |
| `username`, `password_env` | string, string | SMTP auth; `password_env` names an env var, never a literal password. |
| `starttls` | bool | Use STARTTLS. |
| `from` | string | Default `From:` address. |
| `timeout` | duration | SMTP dial/send timeout. |

### `logging`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `level` | `debug` \| `info` \| `warn` \| `error` | `info` | Minimum level for the general application log (not the audit log, which always emits regardless of this). Applied once at startup; a later config reload does not change it live, restart to pick up a change. |
| `format` | `json` \| `text` | `json` | Output format for the general application log. |
| `audit.enabled` | bool | `true` | Whether the structured per-submission audit record is emitted at all. |
| `audit.log_field_values` | bool | `false` | If true, audit records include submitted field *values* (PII), not just metadata. Off by default deliberately — see [Security model](../README.md#security-model). The audit log itself is always JSON regardless of `format` above, that's the point (machine-parseable), so there's no separate `audit.format`. |

### `reload`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `watch_files` | bool | `true` | fsnotify-watch `config.yaml`'s directory and `forms_dir`. |
| `handle_sighup` | bool | `true` | Reload on `SIGHUP`. |

### `metrics`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `true` | Whether `/metrics` is served. `/healthz`/`/readyz` are always served on this listener regardless. |
| `listen_addr` | string | `0.0.0.0:9090` | Internal listener — keep this off the public internet. |
| `path` | string | `/metrics` | Prometheus scrape path. |

### `health`

| Field | Type | Default |
|---|---|---|
| `liveness_path` | string | `/healthz` |
| `readiness_path` | string | `/readyz` |

## `forms.d/<slug>.yaml` (per form)

### Top level

| Field | Type | Default | Meaning |
|---|---|---|---|
| `id` | string | — | Required. Used in the URL (`/f/<id>/submit`) and as the map key — must be unique across `forms_dir`. |
| `display_name` | string | — | Human-readable name, available to templates as `.Form.DisplayName`. |
| `enabled` | bool | `true` | Set `false` to keep a form's config in place but stop serving it (`404`). |
| `allowed_origins` | []string | — | Exact origins (`https://example.com`) or a `https://*.example.com` wildcard-subdomain entry. |
| `channels_required` | `any` \| `all` \| `none` | `any` | What counts as delivery success for the HTTP response: at least one channel, every channel, or don't care (always `200`). |

### `auth`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `site_key` | string | — | Required. A **public** capability token — generate with `formelay keygen`. Not a secret; see [Security model](../README.md#security-model). |
| `transport` | `header` \| `form_field` | `header` | Where the submitted key is read from. `header` avoids leaking the key via `Referer`; use `form_field` only for a plain `<form>` with no JavaScript. |
| `header_name` | string | `X-Formelay-Site-Key` | Header name, when `transport: header`. |
| `form_field_name` | string | — | Required when `transport: form_field`; the form field name carrying the key. |

### `honeypot`

| Field | Type | Meaning |
|---|---|---|
| `field_name` | string | A hidden form field name; a non-empty value here means a bot filled in every field, including ones a human never sees. Empty/unset disables the check. |

### `captcha` (optional)

| Field | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `false` | |
| `provider` | `turnstile` \| `hcaptcha` \| `recaptcha_v2` \| `recaptcha_v3` \| `generic` | — | A named preset fills in `verify_url` and the param/field names below; `generic` requires you to set them yourself. |
| `secret_env` | string | — | Env var holding the provider's server-side secret. |
| `response_field` | string | — | The submitted field name carrying the widget's response token. |
| `on_error` | `fail_open` \| `fail_closed` | `fail_closed` | What happens if the verify call itself errors/times out. This is a hard security gate — unlike the AI classifier's `on_error`, `fail_closed` is the sane default. |
| `verify_url`, `request_encoding` (`form`\|`json`), `secret_param`, `response_param`, `remoteip_param`, `success_field`, `score_field`, `min_score` | — | preset-filled | Override any of these to point at a provider without a named preset — see [Extending with a new provider](examples.md#extending-with-a-new-captcha-provider). |

### `spam_filter` (optional)

| Field | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `false` | |
| `provider.type` | string | — | Currently `ai` (OpenAI-compatible chat-completions). |
| `provider.api_base`, `provider.api_key_env`, `provider.model`, `provider.timeout` | — | — | Provider connection details. |
| `system_template`, `system_template_inline` | string | built-in default | Override the classifier's system prompt. Leave unset to use the embedded, injection-hardened default (see [Security model](../README.md#security-model)). |
| `user_template`, `user_template_inline` | string | built-in default | Override how submitted fields are rendered into the prompt. |
| `include_fields` | []string | `[]` (all fields) | Allowlist restricting which submitted fields are sent to the classifier at all, before templating, so a custom `user_template` can't accidentally leak an excluded field back in. Use it to keep PII (`email`, `phone`, ...) out of the third-party AI call while still classifying on the free-text fields that actually matter (`message`, `subject`). Delivery to `channels` is never affected by this, only the classifier call is. Empty/unset sends every field, unchanged from omitting the option entirely. |
| `on_spam` | `deliver` \| `deliver_tagged` \| `drop` \| `route` | `deliver` | Action when the classifier says `SPAM`. |
| `on_error` | `deliver` \| `deliver_tagged` \| `drop` \| `route` | `deliver` | Action when the classifier call itself fails — configured **independently** of `on_spam`, since a provider outage is "unknown," not "confirmed spam." |
| `route.spam_channels`, `route.error_channels` | []string | `[]` | Channel `id`s (from this form's own `channels`) to notify instead of the normal set, used when the respective action is `route`. Empty means audit-log only. |
| `route.spam_template`, `route.error_template` | string | — | A template shared by every channel in `route.spam_channels`/`error_channels` (see [Delivery templates](#delivery-templates)); `error_template` falls back to `spam_template` if unset. **Required** when the respective action (`on_spam`/`on_error`) is `route` — config validation rejects a form at load/reload time if the needed template is missing. |

`deliver`/`deliver_tagged` continue to the form's normal `channels`; `deliver_tagged` additionally sets `.Meta.SpamSuspected` (and `.Meta.SpamReason`) so a template can flag it. `drop` skips delivery entirely (still audit-logged).

### `rate_limit` (optional override)

`{per_ip: <rate rule>, per_form: <rate rule>}` — either or both override the global `rate_limit.default` for this form only. The global bucket is never overridden per-form.

### `fields`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `required` | []string | `[]` | Field names that must be present and non-empty after sanitization. |
| `validators` | map[string]string | `{}` | Field name → validator name. Built-in validators: `email` (`net/mail.ParseAddress`), `url` (must have a scheme and host), `notblank`. |
| `max_field_length` | int | `5000` | Rune cap per field, applied after sanitization. |

### `channels`

A list of delivery targets:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Unique within the form; used in metrics, audit logs, and `spam_filter.route.*_channels` references. |
| `type` | `email` \| `discord` \| `webhook` | See below. |
| `enabled` | bool | Default `true`. A disabled channel's config (including any `*_env` secret it needs) is never validated or required. |
| `config` | map | Type-specific — see below. |

#### `type: email`

```yaml
config:
  to: ["owner@example.com"]
  host: ...            # optional; inherits smtp_defaults
  port: ...
  username: ...
  password_env: ...
  starttls: ...
  from: ...
  timeout: ...
  subject_template: "subject.tmpl"    # or subject_template_inline
  body_template: "body.tmpl"          # or body_template_inline
  body_type: html                     # html | text
  reply_to_field: email                # optional: submitted field to use as Reply-To,
                                        #   validated with net/mail.ParseAddress (rejected, not
                                        #   just stripped, if it doesn't parse as one address)
```

Any field left unset falls back to `smtp_defaults`.

#### `type: discord`

```yaml
config:
  webhook_url_env: "FORM_X_DISCORD_WEBHOOK"
  timeout: 5s
  template: "discord.tmpl"           # or template_inline — must render a complete
                                       # Discord webhook JSON payload
```

#### `type: webhook`

```yaml
config:
  url: "https://hooks.example.com/..."   # must be https
  method: POST
  headers: {}
  auth:
    type: none        # none | basic | bearer
    username: ...      # basic
    password_env: ...  # basic
    token_env: ...      # bearer
  timeout: 5s
  template: "webhook.tmpl"           # or template_inline
```

Use this for any incoming-webhook-based service (Slack, Telegram, PagerDuty, a custom endpoint) without writing Go code.

## Delivery templates

Referenced by `*_template` (a path, resolved relative to `templates_dir`) or `*_template_inline` (a literal string in the YAML). Parsed once at reload — a broken template fails the reload, not a live request.

- **Email body**: `html/template`, auto-escaped — safe by default even though field values are attacker-controlled.
- **Email subject, Discord, webhook, AI spam-filter prompts**: `text/template`, with a `json` template function you must use explicitly for any field interpolated into a JSON payload (e.g. `{{ .Fields.name | json }}`) — `text/template` has no automatic escaping.
- **`default <fallback> <value>`**: returns `<fallback>` if `<value>` is empty.
- Every template receives:
  - `.Form.ID`, `.Form.DisplayName`
  - `.Fields.<name>` (first value), `.FieldsMulti.<name>` ([]string, for repeated fields like checkboxes)
  - `.Meta.RequestID`, `.Meta.Timestamp`, `.Meta.SourceIP`, `.Meta.Origin`
  - `.Meta.SpamSuspected`, `.Meta.SpamReason`, `.Meta.SpamFilterErr` — populated only after the spam-filter stage runs and only for `deliver_tagged`/`route` outcomes

See [examples.md](examples.md) for complete, working templates.
