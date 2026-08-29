<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/formelay-white.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/formelay-black.png">
    <img src="assets/formelay-black.png" alt="Formelay" width="360">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/bossm8/formelay/actions/workflows/ci.yml"><img src="https://github.com/bossm8/formelay/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
  <a href="https://github.com/bossm8/formelay/releases/latest"><img src="https://img.shields.io/github/v/release/bossm8/formelay?sort=semver" alt="Latest release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/bossm8/formelay" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/bossm8/formelay" alt="License"></a>
</p>

# Formelay

**Receive website form submissions and relay them straight to your inbox.**

Formelay is a lightweight, self-hosted relay for web form submissions. Point a plain HTML `<form>` or a `fetch()` call at it, and it authenticates the request, filters out spam, renders a template you control, and forwards the result to email, Discord, or any webhook-based service.

No dashboard, no database, no lock-in. Just a static binary and a YAML file, doing one job well.

## Why

The case Formelay is built for: a fully static site (a static site generator's output, GitHub Pages, a CDN bucket, whatever) that still needs one genuinely interactive thing, a contact form, a signup box, a support form, without standing up a backend just for that. Point a plain `<form>` on that static site straight at Formelay from the browser and it works: real validation, spam defense, and delivery to your inbox or wherever, with no server code of your own anywhere in the picture.

More generally: Formelay never stores what people submit, and every part of it (routing rules, spam defenses, delivery channels) lives in a config file next to your other infrastructure, rather than in a third-party dashboard. If you'd rather run this piece of your stack yourself than depend on a hosted form-backend service, this is that option.

## Features

- **Makes a static site interactive.** No backend of your own needed: a plain HTML `<form>` on a fully static site can point straight at Formelay and just work.
- **Multi-channel delivery.** Email (SMTP), Discord, and a generic outbound webhook out of the box, plus a clean `Notifier` interface for adding more without touching core code.
- **Layered spam defense.** A free honeypot field, an optional CAPTCHA challenge (one generic verifier config-compatible with Turnstile, hCaptcha, reCAPTCHA v2/v3, and most other providers), and an optional AI content classifier with an injection-hardened default prompt.
- **A security model that's honest about what it can guarantee.** The per-form site key is treated as what it actually is: a public capability token, not a secret, since anything shipped to a browser can't stay confidential. Origin allowlisting, rate limiting, and the layers above are what actually stop abuse.
- **Per-form, per-channel templates.** `html/template` (auto-escaped) for email, `text/template` with explicit JSON escaping for Discord/webhooks/AI prompts.
- **Hot-reloadable YAML config.** Edit `forms.d/*.yaml` or `config.yaml` and it's picked up live (fsnotify or `SIGHUP`), atomically, with the previous config staying in effect if the new one fails to parse or validate.
- **Pluggable rate limiting.** In-memory by default; swap in Valkey to share limits across multiple replicas.
- **Observability from day one.** Prometheus metrics and `/healthz`/`/readyz`, on a listener kept separate from the public submission port.
- **Actually lightweight.** A handful of small dependencies, a distroless multi-stage Docker image, one binary.

## Quick start

This walks through the three example forms shipped in this repo, building the image from source via `docker compose` (no local Go install needed). See [Releases](#releases) to run a published image or binary directly instead.

```bash
cd formelay
cp .env.example .env                        # placeholder credentials so the example forms boot cleanly
docker compose build
docker compose run --rm formelay keygen      # generate a public site key
```

Paste the generated key into `auth.site_key` in each file under `config.example/forms.d/` you plan to try (`contact.yaml`, `newsletter.yaml`, `support.yaml` all ship with the same placeholder), then:

```bash
docker compose up
```

This serves three complete, working example forms side by side (a contact form, a newsletter signup, and a support form with CAPTCHA/AI spam filtering wired up but disabled by default), walked through in detail in [docs/examples.md](docs/examples.md). By default it exposes:

| Port | Purpose |
|---|---|
| `8080` | Public submission API |
| `9090` | `/healthz`, `/readyz`, `/metrics` (keep this internal only in production) |

Try the contact form:

```bash
curl -i -X POST http://localhost:8080/f/contact/submit \
  -H "Origin: https://example.com" \
  -H "X-Formelay-Site-Key: <your public site key>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "name=Alice" \
  --data-urlencode "email=alice@example.com" \
  --data-urlencode "message=Hello there"
```

With the placeholder credentials from `.env.example`, this exercises the full pipeline (auth, rate limiting, honeypot, sanitization, template rendering) and responds `502 delivery_failed`, since the placeholder SMTP/Discord destinations aren't real. Replace them with real credentials in `.env` for actual delivery. See [docs/examples.md](docs/examples.md) for the matching HTML/JS for all three forms, and how to turn on CAPTCHA and AI spam filtering on the support form.

## Examples

[docs/examples.md](docs/examples.md) walks through three real, runnable forms end to end: the YAML config, the templates, and the exact HTML/JS that posts to each one.

- **Contact form**: name/email/message, honeypot, delivery to both email and Discord.
- **Newsletter signup**: a single field, tighter rate limits, posted from JS straight to a Discord channel.
- **Support request**: adds a `subject` field, CAPTCHA, and AI spam filtering (both off by default, with the exact steps to turn them on), plus how to plug in a CAPTCHA provider with no named preset.

## Configuration

One global `config.yaml` (server, security defaults, rate limit backend, SMTP defaults, logging, metrics) plus one YAML file per form under `forms.d/`. Every field is documented in **[docs/configuration.md](docs/configuration.md)**; [`config.example/`](config.example/) has three fully annotated, working configs.

Each form config controls, independently:

- `allowed_origins` and `auth.site_key`: who can submit
- `honeypot` / `captcha` / `spam_filter`: how aggressively to filter spam, and what happens on a spam verdict or a filter provider error (`deliver`, `deliver_tagged`, `drop`, or `route` to a separate review channel, configured independently for a confirmed-spam verdict versus a filter that's unavailable)
- `rate_limit`: per-IP and per-form overrides
- `fields`: required fields and lightweight validators (`email`, `url`, `notblank`)
- `channels`: one or more delivery targets, each with its own template

## Security model

Read this before deploying anything that will see real traffic.

- **The per-form site key is public**, by design. Anything shipped to a browser can be read by anyone who opens dev tools. It scopes and can be revoked/rotated independently of origin config, but it is not a confidentiality boundary.
- **Origin allowlisting** stops casual cross-site reuse but not a scripted request with a forged `Origin` header.
- **Rate limiting** bounds damage from a known or leaked site key.
- **CAPTCHA** is the layer that actually resists a scripted, non-browser attacker. Enable it for any form likely to attract abuse.
- **The AI spam classifier** is a soft content signal, never a security boundary on its own, and ships with a prompt designed to treat prompt-injection attempts in submitted content as spam evidence rather than a bypass.
- Submitted field values are sanitized (real markup stripped via `bluemonday`, invalid UTF-8 rejected, control characters removed, Unicode NFC-normalized via `golang.org/x/text`) and then only ever reach templates as escaped data (`html/template` auto-escaping, an explicit `json` template function for non-HTML targets). They're never re-interpreted as markup, a template source, or a raw HTTP header.
- File uploads are rejected outright. This is a text-field relay, not a file-upload service.

## Development

Go isn't required on your host; build, test, and the Valkey integration suite all run through Docker. Project layout, `make` targets, testing approach, and how to add a new delivery channel/CAPTCHA provider/spam classifier are all in **[docs/develop.md](docs/develop.md)**.

## Releases

Every tagged release publishes, via GoReleaser (`.github/workflows/release.yml`):

- static binaries for linux/darwin on amd64/arm64, with checksums and a changelog, attached to the GitHub Release
- a multi-arch Docker image at `ghcr.io/bossm8/formelay:<version>` and `:latest`

`make release-snapshot` runs the same pipeline locally without publishing anything, useful for verifying a release builds cleanly before tagging.

### Running the Docker image

```bash
docker pull ghcr.io/bossm8/formelay:latest   # or a specific version, e.g. :v0.1.0

docker run -d --name formelay \
  -p 8080:8080 -p 9090:9090 \
  -e SMTP_PASSWORD=... \
  -e FORM_CONTACT_DISCORD_WEBHOOK=... \
  -v "$(pwd)/config.yaml:/etc/formelay/config.yaml:ro" \
  -v "$(pwd)/forms.d:/etc/formelay/forms.d:ro" \
  -v "$(pwd)/templates:/etc/formelay/templates:ro" \
  ghcr.io/bossm8/formelay:latest
```

Every image carries standard OCI labels (title, description, version, revision, build date, license, source/documentation URLs):

```bash
docker inspect --format '{{json .Config.Labels}}' ghcr.io/bossm8/formelay:latest
```

### Running the prebuilt binary

```bash
curl -LO https://github.com/bossm8/formelay/releases/download/v0.1.0/formelay_0.1.0_linux_amd64.tar.gz
curl -LO https://github.com/bossm8/formelay/releases/download/v0.1.0/checksums.txt
sha256sum -c checksums.txt --ignore-missing   # verify before running anything you downloaded

tar -xzf formelay_0.1.0_linux_amd64.tar.gz
./formelay --config /path/to/config.yaml      # metrics/health on :9090, submissions on :8080 by default
./formelay keygen                              # generate a form's public site key
```

Replace `v0.1.0`/`0.1.0` and `linux_amd64` with the actual version and your platform (`darwin_arm64`, `linux_arm64`, ...) from the release's asset list.

## License

[Apache License 2.0](LICENSE).
