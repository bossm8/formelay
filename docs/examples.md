# Examples

Three complete, working forms ship in [`config.example/`](../config.example/)
and run together out of the box via `docker compose up` (see the
[Quick start](../README.md#quick-start)). This page walks through each one: the
config, the templates, and the exact HTML/JS that posts to it. Every
`curl`/`fetch` example below is copy-pasteable against a running local instance.

For the full field-by-field reference, see [configuration.md](configuration.md).
Between them the examples below use all three accepted request formats: a plain
`<form>` and the `curl` calls post `application/x-www-form-urlencoded`, the
`FormData` fetches post `multipart/form-data`, and the newsletter fetch posts
`application/json`. For what each format accepts and every response code, see
[api.md](api.md).

## 1. Contact Form

The simplest real case: a few fields, a honeypot, delivery to both an inbox and
a Discord channel, with CAPTCHA and AI spam filtering present in the config but
switched off until you have real credentials.

**Config**:
[`contact.yaml`](../config.example/forms/contact.yaml)
**Templates**:
[`contact-email-subject.tmpl`](../config.example/templates/contact-email-subject.tmpl),
[`contact-email-body.tmpl`](../config.example/templates/contact-email-body.tmpl),
[`contact-discord.tmpl`](../config.example/templates/contact-discord.tmpl)

```html
<form id="contact" action="https://your-domain/f/contact/submit" method="POST">
  <!-- honeypot: real visitors never see or fill this in -->
  <input type="text" name="website" tabindex="-1" autocomplete="off"
         style="position:absolute;left:-9999px" aria-hidden="true">

  <input type="hidden" name="_key" value="<your public site key>">

  <label>Name <input type="text" name="name" required></label>
  <label>Email <input type="email" name="email" required></label>
  <label>Message <textarea name="message" required></textarea></label>
  <button type="submit">Send</button>
</form>
```

> formelay only supports `POST` on the form endpoint, no `GET`, so make sure
> your HTML form sets `method=POST`.

> That example uses `transport: form_field` (a hidden `_key` input) so it works
> with zero JavaScript: a real `<form>` `POST` straight to formelay. Note that
> formelay always answers with JSON and never redirects, so the browser
> navigates away from your page and lands on the raw response body
> (`{"success":true,...}`). Zero-JS submission works, but there is no thank-you
> page unless you add one yourself. `config.example/forms/contact.yaml` as shipped
> uses `transport: header` instead (the default, and the better choice whenever
> you *do* have JS available). To use the form above, change this field
> (`transport`) to `form_field` and set `form_field_name` to `_key`.

With JS, the matching fetch call becomes:

```js
async function submitContact(form) {
  const res = await fetch("https://your-domain/f/contact/submit", {
    method: "POST",
    headers: {
      "X-Formelay-Site-Key": "<your public site key>"
    },
    body: new FormData(form), // multipart/form-data
  });
  const body = await res.json();
  if (!body.success) throw new Error(body.error);
}
```

To try the contact form example against a running instance:

```bash
curl -i -X POST http://localhost:8080/f/contact/submit \
  -H "Origin: https://example.com" \
  -H "X-Formelay-Site-Key: <your public site key>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "name=Alice" \
  --data-urlencode "email=alice@example.com" \
  --data-urlencode "message=Hello there"
```

## 2. Newsletter Signup

One field, posted from JS, routed straight to a Discord channel. Also the one
example using `response_mode: async` (see
[configuration.md](configuration.md#top-level)): a visitor doesn't need to wait
on the Discord webhook round-trip to see "Subscribed!"

**Config**:
[`config.example/forms/newsletter.yaml`](../config.example/forms/newsletter.yaml)
**Template**:
[`newsletter-discord.tmpl`](../config.example/templates/newsletter-discord.tmpl)

```html
<form id="newsletter">
  <!-- honeypot: real visitors never see or fill this in -->
  <input type="text" name="company" tabindex="-1" autocomplete="off"
         style="position:absolute;left:-9999px" aria-hidden="true">
  <input type="email" name="email" required placeholder="you@example.com">
  <button type="submit">Subscribe</button>
</form>
<script>
  document.getElementById("newsletter").addEventListener("submit", async (e) => {
    e.preventDefault();
    const email = e.target.email.value;
    const res = await fetch("https://your-domain/f/newsletter/submit", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Formelay-Site-Key": "<your public site key>"
      },
      body: JSON.stringify({ email }),
    });
    const body = await res.json();
    alert(body.success ? "Subscribed!" : "Something went wrong.");
  });
</script>
```

## 3. Support Request

The fullest example: a required `subject` field alongside the usual
name/email/message, both extra spam-defense layers present (disabled by default,
see below), and `deliver_tagged` rather than `route` for the spam verdict, so a
suspected-spam ticket still reaches support but visibly flagged, instead of
being pulled out to a separate review channel.
`spam_filter.include_fields: ["subject", "message"]` also means `name`/`email`
never reach the AI provider at all, only the free-text fields relevant to
judging spam do (delivery to email/Discord below still gets every field as
usual)

**Config**:
[`config.example/forms/support.yaml`](../config.example/forms/support.yaml)
**Templates**:
[`support-email-subject.tmpl`](../config.example/templates/support-email-subject.tmpl),
[`support-email-body.tmpl`](../config.example/templates/support-email-body.tmpl),
[`support-discord.tmpl`](../config.example/templates/support-discord.tmpl)

```html
<form id="support">
  <!-- honeypot: real visitors never see or fill this in -->
  <input type="text" name="website" tabindex="-1" autocomplete="off"
         style="position:absolute;left:-9999px" aria-hidden="true">

  <input type="hidden" name="cf-turnstile-response" id="turnstile-token">
  <div class="cf-turnstile" data-sitekey="<your Turnstile site key>"
       data-callback="(token) => document.getElementById('turnstile-token').value = token"></div>

  <label>Name <input type="text" name="name" required></label>
  <label>Email <input type="email" name="email" required></label>
  <label>Subject <input type="text" name="subject" required></label>
  <label>Message <textarea name="message" required></textarea></label>
  <button type="submit">Send</button>
</form>
<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>
<script>
  document.getElementById("support").addEventListener("submit", async (e) => {
    e.preventDefault();
    const res = await fetch("https://your-domain/f/support/submit", {
      method: "POST",
      headers: {
        "X-Formelay-Site-Key": "<your public site key>"
      },
      body: new FormData(e.target),
    });
    const body = await res.json();
    alert(body.success ? "Request Sent" : "Something went wrong.");
  });
</script>
```

The config ships with `captcha.enabled: false` and `spam_filter.enabled: false`
as both need real credentials before they're useful, and validation of an
*enabled* channel/provider requires its `*_env` secret to actually be set. To
turn them on:

1. Get a [Turnstile](https://developers.cloudflare.com/turnstile/) site key +
  secret (or swap `provider: turnstile` for `hcaptcha`/`recaptcha_v2`/`recaptcha_v3`).
2. Set `FORM_SUPPORT_TURNSTILE_SECRET` and `SPAM_FILTER_API_KEY` in `.env` to real values.
3. Flip `captcha.enabled` and `spam_filter.enabled` to `true` in `support.yaml`.
4. Save. formelay picks the change up live, no restart needed (assuming the
   docker container is already up).

## Extending with a new CAPTCHA provider

`turnstile`/`hcaptcha`/`recaptcha_v2`/`recaptcha_v3` are just named presets over
one generic HTTP-based verifier (POST a secret + response token, form-urlencoded
or JSON) to a verify URL, check a boolean success field in the JSON response.
Most other providers (FriendlyCaptcha, GeeTest v3, a self-hosted proof-of-work
challenge like Cap/Altcha, or your own in-house verify endpoint) follow the same
shape, so you can wire one up with **no code change**.

To do so, set `provider: generic` and the override fields yourself:

```yaml
captcha:
  enabled: true
  provider: generic
  secret_env: "FORM_X_MY_CAPTCHA_SECRET"
  response_field: "my-captcha-token"
  verify_url: "https://captcha.example.com/api/verify"
  request_encoding: json          # form | json
  secret_param: "secret"
  response_param: "token"
  remoteip_param: ""              # omit if the provider doesn't want the caller's IP
  success_field: "valid"          # whatever top-level JSON field means "passed"
  on_error: fail_closed
```

A provider with a genuinely different protocol (a signed-token flow, for
instance) needs a real `captcha.Verifier` implementation — see
`internal/captcha/generic.go` for the interface and `cmd/formelay/main.go` for
where implementations are registered.

## Honeypot Tips

A honeypot costs nothing (no network call, no third-party dependency) and
catches the least sophisticated traffic: scripts that fill in every field
they find. It is one layer among several (see the README's
[Security model](../README.md#security-model)), not a replacement for
CAPTCHA or the AI spam filter against anything actually targeted at your
form. A few things make it noticeably more effective in practice, beyond
the basic inline example in [Contact Form](#1-contact-form) above.

**Hide it with layout, not `display: none`, and hide it immediately.** A
`display: none` or `visibility: hidden` field is invisible to people, but
it is also the first thing an unsophisticated bot's own heuristics check
for, and skip. Push it off-screen instead, so it still looks like a real,
fillable field to anything that only inspects the DOM. Put the rule in a
normal, render-blocking stylesheet (a `<link rel="stylesheet">` in
`<head>`, not something injected by JavaScript after the page loads), so
the field is hidden from the very first paint regardless of script
timing or a script failing to run at all. The field itself stays a
normal, always-present element in the HTML source, never added or
removed dynamically:

```css
.honeypot-field {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  left: -9999px;
}
```

**Do not name it "honeypot" anywhere in shipped markup.** A field `id`,
`class`, or HTML comment that literally says `honeypot`, `trap`, or
`do-not-fill` is a dead giveaway to anything that reads your page
source. Give it a name a real form field might plausibly have instead
(`subject`, `company`, `website`), matching whatever you set as
[`honeypot.field_name`](configuration.md#honeypot).

**Set `aria-hidden` and `tabindex="-1"` from JavaScript, not static
HTML, if you can.** Both matter for genuine visitors: `aria-hidden="true"`
keeps screen reader users from ever landing on the field, `tabindex="-1"`
keeps it out of the keyboard tab order, on top of the CSS hiding above.
But some scanning bots do not execute JavaScript at all, they just fetch
the raw HTML and grep for exactly these two attributes as a honeypot
marker. Applying them at runtime, after the page has already loaded,
means the HTML a non-JS bot actually sees carries neither attribute,
indistinguishable from every other field:

```js
var hp = document.querySelector(".honeypot-field");
hp.setAttribute("aria-hidden", "true");
hp.querySelector("input").tabIndex = -1;
```

**Suppress autofill, including password managers.**
`autocomplete="off"` alone is not always enough: LastPass, 1Password,
and Bitwarden each have their own opt-out attribute, and without them a
real visitor's password manager can silently fill the honeypot field
with saved data, tripping the check against a genuine submission:

```html
<input type="text" name="subject" autocomplete="off"
       data-lpignore="true" data-1p-ignore data-bwignore
       data-form-type="other">
```

