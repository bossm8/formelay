> [!CAUTION]
> **Work in progress:** formelay is under active development. Config format and
> behavior may change without notice.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/formelay-white.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/formelay-black.png">
    <img src="assets/formelay-black.png" alt="formelay" width="360">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/bossm8/formelay/actions/workflows/ci.yml">
    <img src="https://github.com/bossm8/formelay/actions/workflows/ci.yml/badge.svg" alt="CI status">
  </a>
  <a href="https://codecov.io/gh/bossm8/formelay">
    <img src="https://codecov.io/gh/bossm8/formelay/graph/badge.svg" alt="Coverage">
  </a>
  <a href="https://github.com/bossm8/formelay/releases/latest">
    <img src="https://img.shields.io/github/v/release/bossm8/formelay?sort=semver" alt="Latest release">
  </a>
  <a href="go.mod">
    <img src="https://img.shields.io/github/go-mod/go-version/bossm8/formelay" alt="Go version">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/bossm8/formelay" alt="License">
  </a>
</p>

# formelay

**A lightweight, self-hosted form relay for static websites**

**formelay** receives web form submissions from forms you define in your own
HTML and relays them to **email**, **Discord**, or **webhook receivers**, with
built-in **validation** and **spam defense**.

It's **stateless**, has **no GUI**, and **no lock-in**. Just a static binary and
a YAML file, doing one job well.

## Why

**formelay** is for fully static websites that still need a form, without having
to run a backend just for that.

Build the form however you like, directly in your website. There is no
predefined form or GUI builder you have to adapt to your needs, and no external
form or script you have to embed. You own the HTML and simply point the form at
formelay.

formelay then takes care of **validation**, **spam defense**, and **delivery**.
No server-side code of your own required.

## Features

- **Multi-Channel Delivery.** Email, Discord, and generic webhooks out of the
  box.
- **Layered Spam Defense.** Honeypot, CAPTCHA, and an AI content classifier.
- **Per-Channel Templates.** Customize how delivered messages appear in your
  inbox.
- **Configurable Response Timing.** Wait for delivery before responding, or
  respond as soon as CAPTCHA passes and deliver asynchronously.
- **Hot-Reloadable Config.** YAML changes apply live, with rollback on invalid
  config.
- **Pluggable Rate Limiting.** Inbound and outbound. In-memory by default, or
  Valkey to share limits across replicas.
- **Built-In Observability.** Prometheus metrics to monitor your form usage.
- **Stateless.** No database or stored submissions, at the cost of delivery
  guarantees in rare cases such as forced restarts. [^1]
- **Lightweight.** Few dependencies, distroless Docker image, single binary.

[^1]: With the optional Valkey rate-limit backend as the exception to store
ephemeral token-bucket counters across replicas, never submission content.

## Quick start

This walks through the three example forms shipped in this repo, building the
image from source via `docker compose` (no local Go install needed). See
[Releases](#releases) to run a published image or binary directly instead.

```bash
cd formelay
cp .env.example .env                        # placeholder credentials so the example forms boot cleanly
docker compose build
docker compose run --rm formelay keygen     # generate a public site key
```

Paste the generated key into `auth.site_key` in each file under
`config.example/forms/` you plan to try (`contact.yaml`, `newsletter.yaml`,
`support.yaml` all ship with the same placeholder), then:

```bash
docker compose up
```

This serves three complete, working example forms side by side (a contact form,
a newsletter signup, and a support form with CAPTCHA/AI spam filtering wired up
but disabled by default), walked through in detail in
**[docs/examples.md](docs/examples.md)**.

By default it exposes:

| Port   | Purpose                                                 |
|--------|---------------------------------------------------------|
| `8080` | Public submission API                                   |
| `9696` | Internal `/healthz`, `/readyz` and `/metrics` endpoints |

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

With the placeholder credentials from `.env.example`, this exercises the full
pipeline (auth, rate limiting, honeypot, sanitization, template rendering) and
responds `502 delivery_failed`, since the placeholder SMTP/Discord destinations
aren't real. Replace them with real credentials in `.env` for actual delivery.
See [docs/examples.md](docs/examples.md) for the matching HTML/JS for all three
forms, and how to turn on CAPTCHA and AI spam filtering on the support form.

## Configuration

One global `config.yaml` (server, security defaults, rate limit backend, SMTP
defaults, logging, metrics) plus one YAML config file per form. Every
field is documented in **[docs/configuration.md](docs/configuration.md)**.

Each form config controls, independently:

- `allowed_origins` and `auth.site_key`: who can submit
- `honeypot` / `captcha` / `spam_filter`: how to filter spam, and what happens
  on a spam verdict
- `rate_limit`: per-IP and per-form overrides
- `fields`: required fields and lightweight validators (`email`, `url`, `regex`,
  `notblank`)
- `channels`: one or more delivery targets, each with its own template

## Metrics

Prometheus metrics are served on `9696` by default. Every metric is documented
with its exact labels and values in
**[docs/metrics.md](docs/metrics.md)**.

## Security model

- **The Per-Form Site Key.** Public by design. Anything shipped to a browser can
  be read by anyone who opens dev tools on that specific site. It is not a
  confidentiality boundary against someone who specifically targets your form,
  but a submission can't succeed without first finding the key on your page, so
  it does stop the bulk of blind, scripted traffic (scanners and copy-pasted
  `curl` payloads that never loaded your site at all). It also scopes and can be
  revoked/rotated independently of origin config.
- **Origin Allowlisting.** Stops casual cross-site reuse but not a scripted
  request with a forged `Origin` header.
- **Rate Limiting.** Bounds damage from a known or leaked site key, inbound as
  well as outbound (email quota, ai spend).
- **CAPTCHA.** Intended to resist scripted, non-browser submissions. Enable it
  for any form likely to attract abuse.
- **The AI Spam Classifier.** A soft content signal, never a security boundary
  on its own. It ships with a prompt designed to treat prompt-injection attempts
  in submitted content as spam evidence rather than a bypass (*you never can be
  sure 100%*). Which fields it sees is configurable and empty by default to
  ensure PII fields (`email`, `phone`, ...) never reach the third-party provider
  by accident.
- **Input Sanitization.** Submitted field values are stripped of markup,
  normalized, and safely escaped before reaching delivery templates. They're never
  interpreted as markup, template source, or raw HTTP headers.
- **Text Only.** File uploads are rejected outright. This is a text-field relay,
  not a file-upload service.

## Development

See **[docs/develop.md](docs/develop.md)**.

## Releases

Every tagged release publishes, via GoReleaser (`.github/workflows/release.yml`):

- Static binaries for `linux/darwin` on `amd64/arm64`, with checksums and a
  changelog, attached to the GitHub Release
- A multi-arch Docker image at `ghcr.io/bossm8/formelay:<version>` and `latest`

### Running the Docker image

```bash
docker pull ghcr.io/bossm8/formelay:latest   # or a specific version, e.g. :v0.1.0

docker run -d --name formelay \
  -p 8080:8080 -p 9696:9696 \
  -e SMTP_PASSWORD=... \
  -e FORM_CONTACT_DISCORD_WEBHOOK=... \
  -v "$(pwd)/config.yaml:/etc/formelay/config.yaml:ro" \
  -v "$(pwd)/forms:/etc/formelay/forms:ro" \
  -v "$(pwd)/templates:/etc/formelay/templates:ro" \
  ghcr.io/bossm8/formelay:latest
```

### Running the prebuilt binary

```bash
curl -LO https://github.com/bossm8/formelay/releases/download/v0.1.0/formelay_0.1.0_linux_amd64.tar.gz
curl -LO https://github.com/bossm8/formelay/releases/download/v0.1.0/checksums.txt
sha256sum -c checksums.txt --ignore-missing   # verify before running anything you downloaded

tar -xzf formelay_0.1.0_linux_amd64.tar.gz
./formelay keygen                             # generate a form's public site key
./formelay --config /path/to/config.yaml      # metrics/health on :9696, submissions on :8080 by default
```

Replace `v0.1.0`/`0.1.0` and `linux_amd64` with the actual version and your
platform (`darwin_arm64`, `linux_arm64`, ...) from the release's asset list.

## License

[Apache License 2.0](LICENSE).
