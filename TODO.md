# TODO

Known limitations of shipped behavior, and the intended fix for each. Nothing
here is half-wired code. Every item below describes something that works today
but could work better. Deferred work belongs in this file rather than in a
`TODO` comment in the source.

User-facing entries are also documented under
[Known limitations](docs/api.md#known-limitations) in the API reference.

## Sanitize `.FieldsMulti`

**Today:** `handleSubmit` sanitizes `flatten(multi)` (first value per field) and
passes the *raw* `multi` map straight to templates as `.FieldsMulti`. So values
past the first skip the length cap, Unicode normalization, control-character
stripping and the invalid-UTF-8 rejection that `.Fields` values get.

**Why it matters:** escaping is unaffected (it happens at render time and covers
both), but a second value can be as long as `max_body_bytes` allows and can
contain control characters or invalid UTF-8.

**Fix:** sanitize every value in `multi`, not just the flattened first values,
and build `.FieldsMulti` from the result. Needs a table-driven test covering a
repeated field whose second value is over-long and one whose second value is
invalid UTF-8. Note it changes rendered output for any template already reading
`.FieldsMulti`.

## JSON cannot express multi-valued fields

**Today:** the JSON body decodes into `map[string]string`, so an array value is
rejected with `400 invalid_body` and duplicate keys are silently collapsed to the
last one by `encoding/json`, with no error and no warning, so the client has no
way to tell that a value was dropped.

**Why it matters:** a checkbox group is perfectly ordinary, and a JS client
posting JSON has no way to send one. The workaround (pack into a delimited
string) then hits a second wall: there is no `split` template function, so the
values cannot be taken apart again for delivery.

**Fix options, in increasing scope:** add a `split` template function so packed
strings are usable; or decode into `map[string]any` and coerce scalars
(string/number/bool → string, `null` → `""`) while still rejecting nested
objects, which would also fix the "`{"count": 3}` is a 400" surprise; or accept
the limitation permanently and leave it documented, as it is now.

Whichever option ships, the silent-collapse case specifically (duplicate JSON
keys losing data with no error) needs a regression test once it no longer
silently loses data — no test pins it as-is in the meantime, since it isn't
correct behavior (see AGENTS.md "Test wanted behavior, not known gaps").

## No-JS submissions land on the raw JSON response

**Today:** every response is JSON; there is no redirect anywhere in the handler.
A `<form>` posted without JavaScript therefore navigates the browser to a page
showing `{"success":true,"request_id":"..."}`.

**Why it matters:** `auth.transport: form_field` exists precisely so a form works
with zero JavaScript, and that path currently ends on a raw JSON document rather
than a thank-you page.

**Fix:** an optional per-form success/error redirect URL, validated against the
form's `allowed_origins` so it cannot be turned into an open redirect.

## `invalid_body` is opaque

**Today:** a missing `Content-Type`, an unsupported one, malformed JSON, non-flat
JSON, a multipart file part and an over-sized body all return the same
`400 invalid_body`. The actual reason is only written to the `debug` log.

**Why it matters:** an integrator seeing `invalid_body` cannot tell whether they
sent the wrong content type or the wrong JSON shape without server log access.

**Fix:** distinct error codes per cause. This extends the public wire contract,
so it needs matching updates to [docs/api.md](docs/api.md) and the status
vocabulary note in [docs/metrics.md](docs/metrics.md).
