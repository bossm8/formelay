# Metrics reference

formelay exposes Prometheus metrics on the internal listener, alongside `/healthz`/`/readyz` — never on the public submission port. Controlled by `metrics.enabled`/`metrics.listen_addr`/`metrics.path` (see [configuration.md](configuration.md#metrics)); the example config serves them at `http://localhost:9090/metrics`.

Every metric below is on a dedicated registry (`internal/metrics`), populated at the exact call site named — nothing here is a placeholder or "reserved for later."

## Submissions & delivery

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `formelay_submissions_total` | counter | `form`, `status` | One increment per submission attempt. `status` is one of `success`, `validation_failed`, `origin_denied`, `auth_denied`, `rate_limited`, `spam_dropped_honeypot`, `captcha_failed`, `spam_dropped_ai`, `delivery_failed` — the same vocabulary as the audit log's `status` field. For a `response_mode: async` form (see [configuration.md](configuration.md#top-level)), this is recorded once the background spam-filter/dispatch work actually finishes, not when the (already-sent) HTTP response went out — so `success` here still means genuinely delivered, even though the client found out sooner. |
| `formelay_deliveries_total` | counter | `form`, `channel`, `channel_type`, `status` | One increment per channel delivery attempt. `channel` is the form's own `channels[].id`; `channel_type` is `email`\|`discord`\|`webhook`; `status` is `success`\|`failure`\|`rate_limited` (this channel's own `rate_limit` — see [configuration.md](configuration.md#rate_limit-optional) — rejected or timed out waiting for capacity; distinct from an actual send failure). |
| `formelay_delivery_latency_seconds` | histogram | `form`, `channel_type` | Time spent in one channel's `Notifier.Send`, including template rendering. |

## Rate limiting

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `formelay_rate_limited_total` | counter | `form`, `scope` | One increment per rejected request. `scope` is `global`\|`per_ip`\|`per_form`. |
| `formelay_ratelimit_buckets_active` | gauge | `scope` | Current in-process bucket count, sampled every 15s. **Memory backend only** — with `rate_limit.backend: valkey`, bucket state lives in Valkey itself, not in this process, so this stays unset. `scope` is `global`\|`ip`\|`form`, taken from the bucket key's own prefix. |
| `formelay_ratelimit_backend_errors_total` | counter | `backend` | Connectivity/timeout errors talking to the rate-limit backend itself (distinct from a normal allow/deny result). **Valkey backend only** — the memory backend never errors, so `backend="memory"` never appears; `on_error` (see [configuration.md](configuration.md#rate_limit)) decides whether such an error still allows the request through. |
| `formelay_ratelimit_outbound_wait_seconds` | histogram | `form`, `channel` | Time spent waiting for an outbound channel rate-limit token. Only observed once actual waiting happened (`rate_limit.on_limit: wait`, see [configuration.md](configuration.md#rate_limit-optional)) — a channel with no `rate_limit` configured, or one whose first check already passed, never appears here. |

## Honeypot, CAPTCHA & spam filter

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `formelay_honeypot_triggered_total` | counter | `form` | One increment per submission dropped by the honeypot field check. |
| `formelay_captcha_verifications_total` | counter | `form`, `provider`, `status` | One increment per CAPTCHA verify call. `provider` is the form's `captcha.provider` (`turnstile`\|`hcaptcha`\|`recaptcha_v2`\|`recaptcha_v3`\|`generic`); `status` is `success`\|`failed`\|`error` (the verify call itself erroring, resolved by `captcha.on_error`). |
| `formelay_spam_filter_verdicts_total` | counter | `form`, `verdict` | One increment per AI classifier call. `verdict` is `not_spam`\|`spam`\|`error`. |
| `formelay_spam_filter_actions_total` | counter | `form`, `trigger`, `action` | One increment per resolved `on_spam`/`on_error` outcome. `trigger` is `spam`\|`error`; `action` is `deliver`\|`deliver_tagged`\|`drop`\|`route`. |
| `formelay_spam_filter_latency_seconds` | histogram | `form` | Time spent in the AI classifier's `Classify` call. |

## Config reload

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `formelay_config_reload_total` | counter | `status` | One increment per reload attempt — the initial load at startup, and every later one triggered by the file watcher (`reload.watch_files`) or `SIGHUP` (`reload.handle_sighup`). `status` is `success`\|`failure`. |
| `formelay_config_last_reload_timestamp_seconds` | gauge | — | Unix timestamp of the last *successful* reload. Set once at startup (the initial load counts), then again on every later success; untouched on failure, so it reflects when the currently-running config was actually loaded, not when a reload was last attempted. |

## Runtime & build

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `formelay_http_requests_in_flight` | gauge | — | Requests currently being handled on the **public** submission listener only (`/f/{formID}/submit`); the internal health/metrics listener isn't instrumented. |
| `formelay_background_dispatches_in_flight` | gauge | — | `response_mode: async` submissions whose AI spam-filter call and delivery are still running in the background, after the HTTP response has already been sent. Stays `0` for any deployment with no `async`-mode form. A sustained climb here means dispatches are piling up faster than they're draining — e.g. a slow or down SMTP/webhook destination. |
| `formelay_build_info` | gauge | `version`, `commit`, `go_version` | Always `1`; join against other series to break them down by build. |

`internal/metrics` also registers the standard Go and process collectors from `prometheus/client_golang/prometheus/collectors` (`go_*`, `process_*`: goroutine count, GC stats, memory, open file descriptors, and so on) — the usual Prometheus Go-runtime metrics, not enumerated individually here.
