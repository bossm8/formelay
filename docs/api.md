# API reference

The submission endpoint's wire contract: what you may send, and what comes back.
For the YAML that configures a form, see [configuration.md](configuration.md);
for complete working forms, see [examples.md](examples.md).

## Endpoint

```http
POST /f/{formID}/submit
```

`{formID}` is the form's `id` (see [configuration.md](configuration.md#top-level)).
`POST` only, there is no `GET`, so an HTML form must set `method="POST"`.
`OPTIONS` is answered for CORS preflight. An unknown or disabled form returns a
plain-text `404`, not JSON.

Checks run in this order, and the **first** one to fail decides the response:

1. origin allowlist → `403`
2. rate limit → `429`
3. body decode → `400`
4. site key → `401`
5. sanitize + validate fields → `400`
6. honeypot → `200` (see [Responses](#responses))
7. silent field validators (a `fields.validators` kind marked `silent:`,
   see [configuration.md](configuration.md#validator-modifiers-not-and-silent)) → `200`
8. CAPTCHA → `400`
9. AI spam filter, then delivery → `200` / `502`

Order matters: a request with a bad `Content-Type` from a disallowed origin gets
`403`, not `400`.

## Request format

Three content types are accepted. The header is parsed with `mime.ParseMediaType`,
so matching is case-insensitive and parameters are tolerated: `application/json;
charset=utf-8` and multipart's required `; boundary=...` both work. Parameters are
otherwise ignored, and bodies are always read as UTF-8.

| `Content-Type` | Multiple values per field | Notes |
|---|---|---|
| `application/x-www-form-urlencoded` | yes | Body only. URL query parameters are ignored. |
| `multipart/form-data` | yes | Text parts only. Any file part rejects the whole submission. |
| `application/json` | no | Flat object, string values only. |

Anything else, including a missing `Content-Type`, is rejected with
`400 invalid_body`.

### JSON

The body must be a **flat JSON object whose values are all strings**:

```json
{ "name": "Alice", "email": "alice@example.com", "message": "Hello there" }
```

Numbers, booleans, arrays and nested objects are **rejected** with
`400 invalid_body`, as is a top-level array or scalar:

```json
{ "count": 3 }                    ✗  number
{ "opt_in": true }                ✗  boolean
{ "tags": ["billing", "outage"] } ✗  array
{ "user": { "name": "Alice" } }   ✗  nested object
{ "name": "Alice" }               ✓
```

`null` is the one quiet exception: it decodes to an empty string, so a *required*
field sent as `null` fails validation (`validation_failed`) rather than decoding.

**Why the restriction:** formelay treats JSON as an alternative *encoding of an
HTML form*, not as a richer data model. Send what a `<form>` would send. A
submission is a flat set of named text fields, and every stage that follows
decoding is defined over exactly that: sanitization, the field validators,
`required`, the honeypot and site-key lookups, the spam filter's field allowlist,
the audit log, and the template data. Accepting nested or typed JSON would mean
each of those controls needs to recurse; keeping the boundary flat keeps them
total.

Note that a rejected body gives you `invalid_body` and nothing more. The specific
reason (unsupported type, malformed JSON, non-string value, file part, size) is
only written to the logs at `debug` level, so check the server log when a `400`
is not obvious.

### Form data

`application/x-www-form-urlencoded` and `multipart/form-data` behave the same
way, except that multipart can carry files and urlencoded cannot.

**File uploads are rejected.** formelay is a text-field relay, not an upload
service. A multipart body containing any part with a `filename` is rejected in
full with `400 invalid_body`, including the text fields alongside it. Strip file
inputs before submitting rather than relying on the server to ignore them.

**Repeated field names are preserved.** A checkbox group or `<select multiple>`
sends one name several times:

```html
<input type="checkbox" name="topics" value="billing">
<input type="checkbox" name="topics" value="outage">
```

which produces the body `topics=billing&topics=outage`. Templates see both views
of it (see [configuration.md](configuration.md#delivery-templates) for the full
template data):

| In a template | Renders |
|---|---|
| `{{ .Fields.topics }}` | `billing` |
| `{{ range .FieldsMulti.topics }}{{ . }} {{ end }}` | `billing outage` |
| `{{ .FieldsMulti.topics \| json }}` | `["billing","outage"]` |

`.FieldsMulti` is the only place the extra values survive. Everything else sees
**the first value only**: `required`, the validators, the honeypot, the site key,
the CAPTCHA token lookup, the AI spam filter and the audit log. Use `.Fields` for
genuinely single-valued fields, `| json` for Discord and webhook targets, and
`range` for email bodies.

**JSON cannot send multiple values for one field.** An array is rejected
outright, and duplicate keys are *silently* collapsed to the last one by Go's
JSON decoder, with no error and no warning:

```json
{ "topics": ["billing", "outage"] }         →  400 invalid_body
{ "topics": "billing", "topics": "outage" } →  accepted, but only "outage" arrives
```

From a JSON submission `.FieldsMulti.<name>` is therefore always a one-element
slice. A client that needs real multi-values must post urlencoded or multipart.
Packing them into one delimited string works for delivery, but note there is no
`split` template function, so you cannot take them apart again in a template.

### Field names

There are no magic field names, no `_redirect`, `_subject` or `_next`. Every
special field is one you name yourself in the form's config:

| Purpose | Config field |
|---|---|
| Honeypot | [`honeypot.field_name`](configuration.md#honeypot) |
| CAPTCHA token | [`captcha.response_field`](configuration.md#captcha-optional) |
| Site key, when `auth.transport: form_field` | [`auth.form_field_name`](configuration.md#auth) |

The one name formelay fixes is a *header*: `X-Formelay-Site-Key`, the default for
`auth.transport: header` (override with `auth.header_name`).

### Limits

| Limit | Setting | Default |
|---|---|---|
| Body size, all content types | [`security.max_body_bytes`](configuration.md#security) | 256 KiB |
| Per-field value length | [`fields.max_field_length`](configuration.md#fields) | 5000 runes |

A body over `max_body_bytes` is rejected with `400 invalid_body`. An over-long
field value is **truncated, not rejected**: the submission still succeeds, with
the value cut to the cap. formelay imposes no limit on the number of fields, nor
on the length of a field *name*; only the body size bounds them (multipart
bodies are additionally subject to Go's own part and header caps).

## Responses

Always `Content-Type: application/json`. The `Accept` header is never consulted,
and there is no redirect mode, so a `<form>` posted without JavaScript navigates
the browser to the raw JSON body.

```json
{ "success": true,  "request_id": "..." }
{ "success": false, "error": "invalid_body", "request_id": "..." }
```

`request_id` is the same id that appears in the server logs and the audit record
for that submission. Quote it when investigating a failure.

| Status | `error` | Cause |
|---|---|---|
| `200` | *(none)* | Accepted. See the note below, this does not always mean delivered. |
| `400` | `invalid_body` | Missing, malformed or unsupported `Content-Type`; malformed or non-flat JSON; a multipart file part; a body over `max_body_bytes`. |
| `400` | `invalid_field_encoding` | A field value was not valid UTF-8. |
| `400` | `validation_failed` | A `required` field was missing or empty, or a validator rejected a value. |
| `400` | `captcha_failed` | The provider rejected the token (a missing token is sent and rejected like any other), or verification errored while [`captcha.on_error`](configuration.md#captcha-optional) is `fail_closed` (the default). |
| `401` | `invalid_site_key` | Missing or wrong `auth.site_key`. |
| `403` | `origin_not_allowed` | `Origin` is not in the form's `allowed_origins`. |
| `429` | `rate_limited` | A global, per-IP or per-form rate limit was hit. |
| `502` | `delivery_failed` | Every delivery channel failed. |
| `503` | `not_ready` | Config is not loaded yet; retry. |
| `404` | *plain text, not JSON* | Unknown or disabled form. |

**`200` does not always mean delivered**, by design:

- A submission that trips the honeypot, fails a validator marked `silent:`, or
  that the AI spam filter drops, gets an ordinary success response, because a
  bot should not learn that it was caught.
- With [`response_mode: async`](configuration.md#top-level), the response is sent
  before delivery is attempted, so a later failure is never visible to the client.

In both cases the real outcome is in the audit log and in
`formelay_submissions_total` (see [metrics.md](metrics.md)). Note that the metric
and audit `status` vocabulary is *not* the same as the `error` strings above: for
example `origin_denied` there versus `origin_not_allowed` on the wire.

## Known limitations

- **`.FieldsMulti` values are not sanitized.** Sanitization runs on the first
  value of each field, so values past the first reach templates raw:

  | | `.Fields` | `.FieldsMulti` |
  |---|---|---|
  | markup stripped | yes | no |
  | Unicode normalized, control characters stripped | yes | no |
  | capped at `max_field_length` | yes | no |
  | invalid UTF-8 rejected | yes | no |

  Escaping is unaffected, since it happens at render time (`html/template`
  auto-escaping, or the `json` function) and covers both. But a second value can
  be as long as the whole body allows, and can contain control characters or
  invalid UTF-8. Apply `{{ truncate 5000 . }}` when interpolating multi-values
  into a template.
- **No `split` template function**, so a delimited string cannot be expanded into
  a list in a template.
- **`invalid_body` is opaque**: five distinct causes share one code, with the
  detail only in the `debug` log.

These are tracked, with the intended fix for each, in
[TODO.md](../TODO.md).
