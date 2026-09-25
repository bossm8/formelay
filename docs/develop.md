# Developing formelay

## Project layout

```text
cmd/formelay/         entrypoint: wiring, keygen/healthcheck subcommands
internal/config/      YAML schema, loading, validation
internal/app/         composition root: the single atomically swapped runtime
internal/api/         HTTP handlers: the submission pipeline
internal/notify/      Notifier interface plus email/discord/webhook
internal/captcha/     generic CAPTCHA verifier and provider presets
internal/spamfilter/  AI content classifier
internal/ratelimit/   memory and Valkey rate limit backends
internal/render/      template parsing and execution
internal/sanitize/    input normalization
internal/audit/       structured submission logging
internal/metrics/     Prometheus collectors
```

## Setup

Go isn't required on your host; everything runs through Docker.

```bash
make build                  # go build ./...
make vet                    # go vet ./...
make test                   # go test ./...
make coverage               # go test -coverprofile=coverage.out ./..., prints the total
make race                   # go test -race ./...
make tidy                   # go mod tidy
make vulncheck              # govulncheck ./...
make deadcode               # whole-program reachability check (incl. tests) via golang.org/x/tools/cmd/deadcode
make test-integration       # ratelimit/valkey, email, and api suites against real Valkey/Mailpit/webhook-mirror, via docker-compose.test.yml
make test-live              # captcha suite against the real Turnstile/hCaptcha verify endpoints
make release-snapshot       # local goreleaser dry run (binaries + Docker images, no publish)
```

A `.devcontainer/` is included if you'd rather develop inside a container directly.

## Local form testing

Testing your own form against a locally running instance often runs into
`allowed_origins` rejecting the origin you're actually testing from (`file://`
pages report `Origin: null`, e.g. when opening a simple html form; a dev-server
port varies). `allowed_origins: [DANGEROUS_DISABLED]` disables origin/CORS
checking entirely for that form, purely for this. Every reload while it's set
logs a loud warning naming the form. **This is for development only, never set
this on a real deployment.**

## GitHub Actions

GitHub Actions runs gofmt/vet/build/`-race`-tests (with coverage uploaded to
[Codecov](https://codecov.io/gh/bossm8/formelay))/`govulncheck`/`deadcode`/the
integration suite (Valkey, email, api)/the CAPTCHA live suite on every push and
pull request (`.github/workflows/ci.yml`).

The live-provider job is `continue-on-error`: a genuine regression still shows
up clearly, but a Cloudflare/hCaptcha outage doesn't block an unrelated PR.

`deadcode` blocks the build: unlike the third-party-dependent live suite, an
unreachable-code finding is a real regression in our own code.

## Testing approach

- Unit tests live next to the code they cover and run with `make test`/
  `make race`; no network or Docker needed (except for the devcontainer when no local go
  toolchain is installed).
- `internal/ratelimit/valkey` additionally has an `integration` build-tagged
  suite (`internal/ratelimit/valkey/integration_test.go`) that runs against a real
  Valkey, driven by `make test-integration` via `docker-compose.test.yml`.
- `internal/notify/email` additionally has an `integration` build-tagged suite
  (`internal/notify/email/integration_test.go`) that sends real mail via a
  disposable [Mailpit](https://mailpit.axllent.org/) instance (also started by
  `docker-compose.test.yml`) and asserts on what it actually received
  (To/From/Subject/Reply-To/body) through Mailpit's JSON API.
- `internal/api` additionally has an `integration` build-tagged suite
  (`internal/api/integration_test.go`) that drives real HTTP submissions through
  channels wired to Mailpit and to a small custom recorder (`cmd/webhookmirror`,
  also started by `docker-compose.test.yml`) standing in for the webhook/discord
  receiving end, covering normal delivery, `on_spam: deliver_tagged`, and
  `on_spam: route`.
- `internal/captcha` additionally has a `live` build-tagged suite
  (`internal/captcha/live_test.go`, `make test-live`) that calls the real
  Turnstile and hCaptcha verify endpoints using each provider's official public
  test key pairs, no account or secret of your own needed.

## Extending formelay

The channel, CAPTCHA, and spam-classifier subsystems are each a small `Registry`
(`type: string` to Go constructor) plus a shared interface, following the same
shape:

- **A new delivery channel**: implement `notify.Notifier`
  (`Send(ctx, RenderedMessage) error`, `Type() string`) in a new
  `internal/notify/<name>` package, and optionally `notify.TemplateProvider`
  if it needs templates rendered for it. Register it in `cmd/formelay/main.go`
  alongside `email`/`discord`/`webhook`. For anything that's just "POST JSON to
  an incoming webhook", you likely don't need this at all, the built-in
  `webhook` channel already covers Slack/Telegram/PagerDuty/etc. with zero code
  (see [examples.md](examples.md)).
- **A new CAPTCHA provider**: most providers speak the same reCAPTCHA-derived
  protocol as Turnstile/hCaptcha/reCAPTCHA, so `provider: generic` with the
  right `verify_url`/param names usually needs no code at all, see
  [Extending with a new CAPTCHA provider](examples.md#extending-with-a-new-captcha-provider).
  A provider with a genuinely different protocol needs a real `captcha.Verifier`
  implementation (`internal/captcha/generic.go` has the interface).
- **A new spam-filter provider**: implement `spamfilter.Classifier`
  (`Classify(ctx, render.SubmissionData) (Verdict, error)`, `Type() string`)
  in a new `internal/spamfilter/<name>` package and register it the same way as
  a channel.

In all three cases, config decoding for a new implementation is a YAML
round-trip (see `internal/yamlutil.Decode`), not a new code path in
`internal/config`, that package stays generic across every implementation.
