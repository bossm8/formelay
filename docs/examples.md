# Examples

Three complete, working forms ship in [`config.example/`](../config.example/) and run together out of the box via `docker compose up` (see the [Quick start](../README.md#quick-start)). This page walks through each one: the config, the templates, and the exact HTML/JS that posts to it. Every `curl`/`fetch` example below is copy-pasteable against a running local instance.

For the full field-by-field reference, see [configuration.md](configuration.md).

## 1. Contact form — the baseline

The simplest real case: a few fields, a honeypot, delivery to both an inbox and a Discord channel, with CAPTCHA and AI spam filtering present in the config but switched off until you have real credentials.

**Config**: [`config.example/forms/contact.yaml`](../config.example/forms/contact.yaml)
**Templates**: [`contact-email-subject.tmpl`](../config.example/templates/contact-email-subject.tmpl), [`contact-email-body.tmpl`](../config.example/templates/contact-email-body.tmpl), [`contact-discord.tmpl`](../config.example/templates/contact-discord.tmpl)

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

That example uses `transport: form_field` (a hidden `_key` input) so it works with zero JavaScript — a real `<form>` POST straight to formelay, page-navigation redirect and all. `config.example/forms/contact.yaml` as shipped uses `transport: header` instead (the default, and the better choice whenever you *do* have JS available — see [Security model](../README.md#security-model) on why `header` avoids a `Referer`-based leak that `form_field` and URL-embedded tokens don't). The matching fetch call:

```js
async function submitContact(form) {
  const res = await fetch("https://your-domain/f/contact/submit", {
    method: "POST",
    headers: { "X-Formelay-Site-Key": "<your public site key>" },
    body: new FormData(form), // multipart/form-data — works with <input type=file> present too;
                                // formelay itself rejects any file part with 400, text fields only
  });
  const body = await res.json();
  if (!body.success) throw new Error(body.error);
}
```

Try it against a running instance:

```bash
curl -i -X POST http://localhost:8080/f/contact/submit \
  -H "Origin: https://example.com" \
  -H "X-Formelay-Site-Key: <your public site key>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "name=Alice" \
  --data-urlencode "email=alice@example.com" \
  --data-urlencode "message=Hello there"
```

## 2. Newsletter signup — minimal, high-abuse-risk

One field, posted from JS, routed straight to a Discord channel your marketing team watches — no email round-trip needed for something this lightweight. Tighter rate limits and a different honeypot field name (`company` reads naturally on a signup box) than the contact form, since public signup boxes attract more automated abuse per real visitor than a form someone deliberately fills in. Also the one example using `response_mode: async` (see [configuration.md](configuration.md#top-level)): a visitor doesn't need to wait on the Discord webhook round-trip to see "Subscribed!"

**Config**: [`config.example/forms/newsletter.yaml`](../config.example/forms/newsletter.yaml)
**Template**: [`newsletter-discord.tmpl`](../config.example/templates/newsletter-discord.tmpl)

```html
<form id="newsletter">
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
    headers: { "Content-Type": "application/json", "X-Formelay-Site-Key": "<your public site key>" },
    body: JSON.stringify({ email }),
  });
  const body = await res.json();
  alert(body.success ? "Subscribed!" : "Something went wrong.");
});
</script>
```

## 3. Support request — CAPTCHA + AI spam filtering

The fullest example: a required `subject` field alongside the usual name/email/message, both extra spam-defense layers present (disabled by default, see below), and `deliver_tagged` rather than `route` for the spam verdict, so a suspected-spam ticket still reaches support but visibly flagged, instead of being pulled out to a separate review channel. `spam_filter.include_fields: ["subject", "message"]` also means `name`/`email` never reach the AI provider at all, only the free-text fields relevant to judging spam do; delivery to email/Discord below still gets every field as usual.

**Config**: [`config.example/forms/support.yaml`](../config.example/forms/support.yaml)
**Templates**: [`support-email-subject.tmpl`](../config.example/templates/support-email-subject.tmpl), [`support-email-body.tmpl`](../config.example/templates/support-email-body.tmpl), [`support-discord.tmpl`](../config.example/templates/support-discord.tmpl)

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
    headers: { "X-Formelay-Site-Key": "<your public site key>" },
    body: new FormData(e.target),
  });
  const body = await res.json();
  alert(body.success ? "Sent!" : "Something went wrong.");
});
</script>
```

This one ships with `captcha.enabled: false` and `spam_filter.enabled: false` — both need real credentials before they're useful, and validation of an *enabled* channel/provider requires its `*_env` secret to actually be set (a disabled one requires nothing, which is exactly why `docker compose up` boots cleanly with only placeholder values in `.env.example`). To turn them on:

1. Get a [Turnstile](https://developers.cloudflare.com/turnstile/) site key + secret (or swap `provider: turnstile` for `hcaptcha`/`recaptcha_v2`/`recaptcha_v3` — same shape, different preset).
2. Set `FORM_SUPPORT_TURNSTILE_SECRET` and `SPAM_FILTER_API_KEY` in `.env` to real values.
3. Flip `captcha.enabled` and `spam_filter.enabled` to `true` in `support.yaml`.
4. Save — formelay picks the change up live, no restart needed. If either value doesn't parse or a secret is genuinely missing, the reload is rejected and logged, and the *previous* (working) config keeps serving.

## Extending with a new CAPTCHA provider

`turnstile`/`hcaptcha`/`recaptcha_v2`/`recaptcha_v3` are just named presets over one generic HTTP-based verifier — POST a secret + response token (form-urlencoded or JSON) to a verify URL, check a boolean success field in the JSON response. Most other providers (FriendlyCaptcha, GeeTest v3, a self-hosted proof-of-work challenge like Cap/Altcha, or your own in-house verify endpoint) follow the same shape, so you can wire one up with **no code change** — set `provider: generic` and the override fields yourself:

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

A provider with a genuinely different protocol (a signed-token flow, for instance) needs a real `captcha.Verifier` implementation — see `internal/captcha/generic.go` for the interface and `cmd/formelay/main.go` for where implementations are registered.
