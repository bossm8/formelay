# Configuration reference

formelay reads two kinds of YAML: one global `config.yaml`, and one file per
form under `forms_dir` (`forms/*.yaml` by default). Both are strictly decoded
and hot-reloaded (fsnotify watching the directories, `SIGHUP` or a HTTP `POST`
to the internal reload endpoint). A new config is fully validated before
replacing a current one, including parsing every template it references. An
invalid change is rejected and logged while the previous config keeps serving.

Working examples of the settings below are documented in [examples.md](examples.md).

## Global `config.yaml`

### `server`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `listen_addr` | string | `0.0.0.0:8080` | Public submission API listener. |
| `read_timeout`, `write_timeout`, `idle_timeout`, `read_header_timeout` | duration | `read_header_timeout` falls back to `5s` if unset; others `0` (no timeout) | Standard `net/http.Server` timeouts. |
| `shutdown_grace_period` | duration | `15s` | How long graceful shutdown waits for in-flight requests. If aync delivery is enabled, this should be set long enough to ensure all forms still in-flight are delivered correctly as the sender has no failure feedback in that case. |
| `tls.enabled`, `tls.cert_file`, `tls.key_file` | bool, string, string | — | If enabled, the public submission listener terminates TLS itself (`cert_file`/`key_file` are required and must exist, checked at config load). Only the public listener is affected, the internal health/metrics listener always stays plain HTTP. |
| `trusted_proxies` | []string | `[]` | CIDRs (or bare IPs, treated as `/32`/`/128`) allowed to set `X-Forwarded-For`. |

### `forms_dir`, `templates_dir`

Paths to the per-form YAML directory and the directory template `path:`
references resolve against.
Defaults: `/etc/formelay/forms`, `/etc/formelay/templates`

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
| `valkey` | map[string]object | `{}` | Valkey configuration, required when `backend: valkey` - see below. |
| `outbound_buckets` | map[string]object | `{}` | Named, shared outbound rate-limit buckets — see below. |

#### `valkey`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `addresses` | []string | — | Where Valkey is reachable. |
| `password_env` | string | — | Env var holding the Valkey password (empty = no auth). |
| `db` | int | `0` | Valkey `SELECT` database index. |
| `dial_timeout` | duration | client default | Connection timeout. |
| `key_prefix` | string | `""` | Prefixed onto every rate-limit key, useful if multiple services share one Valkey. |
| `on_error` | `allow` \| `deny` | `allow` | What happens to a request if Valkey itself is unreachable *after* startup (see [Rate limiting](#rate-limiting)). |

#### `outbound_buckets`

Shared rate-limit buckets which can be referenced by outbound channels and ai
spam classifiers in case multiple forms use the same provider. A channel's or
spam filter's `rate_limit` block can reference one of the shared buckets via
`shared_key` instead of defining it's own bucket. The configuration is the same
as the channel's oubound [`rate_limit`](#rate_limit-optional).

```yaml
rate_limit:
  outbound_buckets:
    primary-smtp:
      rate: 10
      window: 1m
      burst: 10
      on_limit: wait
      max_wait: 5s
```

On a channel, or spam_filter, the `primary-smtp` can then be referenced:

```yaml
rate_limit:
  shared_key: "primary-smtp"
```

> **`shared_key` and the inline fields
  (`rate`/`window`/`burst`/`on_limit`/`max_wait`) are mutually exclusive**

#### Rate rules

```yaml
{rate: <float>, window: <duration>, burst: <float>}
```

A rate rule is a token bucket refilling at `rate` tokens per `window`, holding
at most `burst` tokens.

Example: `{rate: 5, window: 1m, burst: 5}` allows a burst of 5 immediately, then
steady-state 5/minute.

#### Rate limiting

- **`memory`**: in-process, sharded token buckets. Ok for a single running
  instance as state is lost on restart and isn't shared across replicas.
- **`valkey`**: bucket state lives in [Valkey](https://valkey.io), updated
  atomically via a Lua script, to ensure a consistent state accross replicas.
  Connection is established at startup and an unreachable Valkey at boot is
  treated as startup failure. `on_error` then governs a runtime outage: `allow`
  (default) degrades to non rate-limited (allowing all requests) while
  `deny` rejects all requests.

### `smtp_defaults`

Inherited by any `email` channel that doesn't override the same field itself.
It's optional and recommendation is to leave it unsed in multi-tenant like
deployments where form configs are not necessarily managed by the operator.

| Field | Type | Meaning |
|---|---|---|
| `host` | string | SMTP server address. |
| `port` | int | SMTP server port. |
| `username` | string | SMTP auth username |
| `password_env` | string | SMTP auth password env var, never a literal password. |
| `starttls` | bool | Use STARTTLS. |
| `from` | string | Default `From:` address. |
| `timeout` | duration | SMTP dial/send timeout. |

### `logging`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `level` | `debug` \| `info` \| `warn` \| `error` | `info` | Minimum level for the general application log. Applied once at startup, i.e. a later config reload does not change it live, restart to pick up a change. |
| `format` | `json` \| `text` | `json` | Output format for the general application log. |
| `audit.enabled` | bool | `true` | Enable/Disable structured (json) audit logs. |
| `audit.log_field_values` | bool | `false` | If true, audit records include submitted field *values* (and thus potentially PII), not just metadata. The audit log itself is always JSON regardless of `format` above. Enable only if strictly required |

### `reload`

Configure how formelay reloads configuration changes.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `watch_files` | bool | `true` | fsnotify-watch `config.yaml`'s directory and `forms_dir`. |
| `handle_sighup` | bool | `true` | Reload on `SIGHUP`. |
| `handle_http` | bool | `true` | Enable serving `http_path` on the internal listener. |
| `http_path` | string | `/reload` | The path where to `POST` to trigger a reload on demand. Only applies when `handle_http` is true and needs a non-empty value in that case. Responds `200 {"success":true}`, or `500 {"success":false,"error":"..."}` with the validation error if the new config is rejected . Served on `internal.listen_addr`. |

> For all reload mechanisms: when reloading fails with an error, the previous
config keeps serving

### `internal`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `listen_addr` | string | `0.0.0.0:9696` | Bind address for the internal listener. Serves `/healthz`, `/readyz`, `reload.http_path` (if enabled), and `/metrics` (if enabled). **Keep this off the public internet.** |

### `metrics`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `true` | Enable the `metrics.path` endpoint for serving Prometheus metrics on `internal.listen_addr`. |
| `path` | string | `/metrics` | Prometheus scrape path. Required non-empty when `metrics.enabled` is `true`. |

### `health`

Served on `internal.listen_addr`.

| Field | Type | Default |
|---|---|---|
| `liveness_path` | string | `/healthz` |
| `readiness_path` | string | `/readyz` |

## `forms/<slug>.yaml` (per form)

### Top level

| Field | Type | Default | Meaning |
|---|---|---|---|
| `id` | string | — | Required. Used in the URL (`/f/<id>/submit`) and as the map key and must thus be unique across `forms_dir`. |
| `display_name` | string | — | Human-readable name, available to templates as `.Form.DisplayName`. |
| `enabled` | bool | `true` | Set `false` to keep a form's config in place but stop serving it (results in `404`). |
| `allowed_origins` | []string | — | Exact origins (`https://example.com`) or a `https://*.example.com` wildcard-subdomain entry. |
| `channels_required` | `any` \| `all` \| `none` | `any` | What counts as delivery success for the HTTP response: at least one channel, every channel, or don't care (always `200`). **Only affects the response in `response_mode: sync`**. |
| `response_mode` | `sync` \| `async` | `sync` | `sync` (default): the HTTP response waits for the AI spam filter and delivery to actually finish. `async`: the response is sent immediately once CAPTCHA passes (CAPTCHA itself is always synchronous, in either mode), and the AI spam filter + delivery run in a background goroutine. In `async` mode, a `200` response means "accepted," not "delivered" — the real outcome (`success`, `spam_dropped_ai`, `delivery_failed`) is only visible via the audit log and `formelay_submissions_total`, arriving after the response. Submissions still in flight when formelay shuts down get up to `server.shutdown_grace_period` to finish before being abandoned. |

### `auth`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `site_key` | string | — | Required. A **public** capability token which can be generatet with `formelay keygen`. This is **not a secret** against a targeted attacker, but required to submit at all. See [Security model](../README.md#security-model). |
| `transport` | `header` \| `form_field` | `header` | Where the submitted key is read from. Use `form_field` only for a plain `<form>` with no JavaScript. |
| `header_name` | string | `X-Formelay-Site-Key` | Header name, when `transport: header`. |
| `form_field_name` | string | — | Required when `transport: form_field` to name the form field name carrying the key. |

### `honeypot`

| Field | Type | Meaning |
|---|---|---|
| `field_name` | string | A hidden form field name. A non-empty value submitted here means a bot filled in a field a human does not see (needs to be configured accordingly in the form). Empty/unset disables the check. |

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
| `system_template`, `system_template_inline` | string | built-in default | Override the classifier's system prompt. Leave unset to use the [embedded default](../internal/spamfilter/ai/defaults/) (see [Security model](../README.md#security-model)). This can be helpful if you want to adjust e.g. suspected spam instructions tailored to your form content. The model however must output exactly two lines as the default template describes. |
| `user_template`, `user_template_inline` | string | built-in default | Override how submitted fields are rendered into the prompt. |
| `include_fields` | []string | `[]` (no fields) | Allowlist restricting which submitted fields are sent to the classifier at all, before templating, so a custom `user_template` can't accidentally leak an excluded field back in. **Privacy-safe by default: empty/unset sends zero fields**, not every field — the AI classifier effectively does nothing useful until you explicitly list which fields it should see. Use it to send only the free-text fields that actually matter for judging spam (`message`, `subject`, ...) while keeping PII (`email`, `phone`, ...) out of the third-party AI call entirely. Delivery to `channels` is never affected by this, only the classifier call is. |
| `on_spam` | `deliver` \| `deliver_tagged` \| `drop` \| `route` | `deliver` | Action when the classifier says `SPAM`. |
| `on_error` | `deliver` \| `deliver_tagged` \| `drop` \| `route` | `deliver` | Action when the classifier call itself fails — configured **independently** of `on_spam`, since a provider outage is "unknown," not "confirmed spam." |
| `route.spam_channels`, `route.error_channels` | []string | `[]` | Channel `id`s (from this form's own `channels`) to notify instead of the normal set, used when the respective action is `route`. Empty means audit-log only. |
| `route.spam_template`, `route.error_template` | string | — | A template shared by every channel in `route.spam_channels`/`error_channels` (see [Delivery templates](#delivery-templates)); `error_template` falls back to `spam_template` if unset. **Required** when the respective action (`on_spam`/`on_error`) is `route` — config validation rejects a form at load/reload time if the needed template is missing. |
| `rate_limit` | object | unset (no limiting) | Throttles calls to the classifier — see below. |

`deliver`/`deliver_tagged` continue to the form's normal `channels`; `deliver_tagged` additionally sets `.Meta.SpamSuspected` (and `.Meta.SpamReason`) so a template can flag it. `drop` skips delivery entirely (still audit-logged).

#### `spam_filter.rate_limit` (optional)

Throttles calls to the AI classifier itself — the same block, and the same two mutually exclusive shapes (inline numbers, or `shared_key` referencing a [`rate_limit.outbound_buckets`](#outbound_buckets) entry), as a channel's outbound [`rate_limit`](#rate_limit-optional) (see that section for the full field reference), reused here because a classifier call is exactly the same kind of rate-limited/cost-bearing third-party call a delivery channel makes:

```yaml
spam_filter:
  enabled: true
  rate_limit:
    rate: 10
    window: 1m
    burst: 5
    on_limit: fail
```

**An exceeded limit (or a timed-out `on_limit: wait`) is resolved exactly like any other classifier failure — through the form's own `spam_filter.on_error`, not a separate action.** An operator who already decided what "the classifier is unavailable" means for their form (`deliver`, `drop`, `route`, ...) doesn't have to decide it a second time for "the classifier is unavailable because we throttled it ourselves." When this happens, the real `Classify` call to the AI provider is never made.

`on_limit: wait` blocks the submission (up to `max_wait`) waiting for capacity, same tradeoff as a channel's outbound wait — formelay has no queue, so this is the only way to avoid resolving via `on_error` under a legitimate burst; `on_limit: fail` resolves via `on_error` immediately instead. Either way it's observed in `formelay_ratelimit_outbound_wait_seconds{target="spam_filter"}` (wait time, `on_limit: wait` only) and `formelay_spam_filter_actions_total{trigger="error"}` (the resolved action). Use `shared_key` if the spam filter should share one bucket with a channel (or another form's spam filter) genuinely billed against the same provider account/quota — see [`rate_limit.outbound_buckets`](#outbound_buckets) for how the shared bucket is defined and why it can't be combined with inline numbers.

### `rate_limit` (optional override)

`{per_ip: <rate rule>, per_form: <rate rule>}` — either or both override the global `rate_limit.default` for this form only. The global bucket is never overridden per-form.

### `fields`

| Field | Type | Default | Meaning |
|---|---|---|---|
| `required` | []string | `[]` | Field names that must be present and non-empty after sanitization. Any submitted field *not* listed here is optional: accepted if present, silently ignored if omitted — there's no separate "declare an optional field" setting, absence from `required` is what makes a field optional. |
| `validators` | map[string]string | `{}` | Field name → validator name. Built-in validators: `email` (`net/mail.ParseAddress`), `url` (must have a scheme and host), `notblank`; or a custom pattern via `regex:<pattern>` (see below). Applies to optional fields too, but only fires on submissions where the field is actually present. An unrecognized validator name (a typo, or neither a built-in nor `regex:...`) is a config-load error, not a silent no-op. |
| `max_field_length` | int | `5000` | Rune cap per field, applied after sanitization. |

**Custom validators**: `regex:<pattern>` matches the field value against a Go (RE2) regular expression — no automatic `^...$` anchoring, the pattern controls that itself, so `regex:hello` matches anywhere `hello` appears in the value, same as plain Go `regexp.MatchString`. The pattern is checked for valid syntax at config load (a broken regex fails the reload, not a live request) and compiled once, cached by pattern text, not recompiled per submission. Prefer single-quoted YAML for the pattern to avoid backslash-escaping fights with YAML's double-quoted-string escaping:
```yaml
fields:
  validators:
    zip: 'regex:^\d{5}$'
```

### `channels`

A list of delivery targets:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Unique within the form; used in metrics, audit logs, and `spam_filter.route.*_channels` references. |
| `type` | `email` \| `discord` \| `webhook` | See below. |
| `enabled` | bool | Default `true`. A disabled channel's config (including any `*_env` secret it needs) is never validated or required. |
| `rate_limit` | object | Optional. Throttles *outbound* deliveries on this channel — see below. |
| `config` | map | Type-specific — see below. |

#### `rate_limit` (optional)

Independent of the form-level `rate_limit` above (which throttles *incoming* submissions) — this throttles how often formelay actually sends *out* on this one channel, so a burst of legitimate submissions can't blow through a mail provider's or webhook's sending quota. Unset: no outbound limiting, unchanged from before this existed.

Two mutually exclusive shapes — see [`rate_limit.outbound_buckets`](#outbound_buckets) for why they can't be combined:

```yaml
# this channel gets its own private bucket:
rate_limit:
  rate: 10          # required: tokens per window
  window: 1m         # required
  burst: 10           # required: max burst size
  on_limit: wait          # "wait" (default) | "fail"
  max_wait: 5s              # only used when on_limit is "wait" (its default
                              #   too, if unset); how long to block for a
                              #   token before giving up as a failed delivery
```

```yaml
# or: draw from a bucket shared with other channels/the spam filter,
# defined once under the global rate_limit.outbound_buckets:
rate_limit:
  shared_key: "primary-smtp"
```

`rate`/`window`/`burst` use the same token-bucket semantics as the [rate rules](#rate-rules) above. `on_limit: wait` blocks (up to `max_wait`) for capacity before sending — formelay has no queue, so this is the only way to avoid dropping a delivery outright under a legitimate burst; `on_limit: fail` fails the delivery immediately instead, with zero added latency. Either way, an exceeded limit shows up as `status="rate_limited"` in `formelay_deliveries_total` and the audit log, distinct from an actual send failure. This uses the same backend as `rate_limit.backend` above (`memory` or `valkey`) — with `valkey`, an outbound limit is automatically shared across replicas too, which matters once there's more than one formelay instance hitting the same provider quota.

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
