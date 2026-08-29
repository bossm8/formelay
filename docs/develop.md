# Developing Formelay

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

`internal/app` is the composition root: it owns the single atomically-swapped runtime that combines raw config with everything built from it (notifiers, verifiers, classifiers, parsed templates), so a reader can never observe config and compiled objects that disagree with each other, even momentarily. `internal/config` itself stays decoupled from `internal/notify`/`internal/captcha`/`internal/spamfilter`: it never imports them, so adding a channel or provider never means touching the config loader.

## Setup

Go isn't required on your host; everything runs through Docker.

```bash
make build             # go build ./...
make vet                # go vet ./...
make test                 # go test ./...
make race                  # go test -race ./...
make tidy                   # go mod tidy
make vulncheck               # govulncheck ./...
make test-integration        # ratelimit/valkey suite against a real Valkey, via docker-compose.test.yml
make release-snapshot        # local goreleaser dry run (binaries + Docker images, no publish)
```

A `.devcontainer/` is included if you'd rather develop inside a container directly.

GitHub Actions runs gofmt/vet/build/`-race`-tests/`govulncheck`/the Valkey integration suite on every push and pull request (`.github/workflows/ci.yml`).

## Testing approach

- Unit tests live next to the code they cover and run with `make test`/`make race`; no network or Docker needed.
- `internal/ratelimit/valkey` additionally has an `integration` build-tagged suite (`internal/ratelimit/valkey/integration_test.go`) that only runs against a real Valkey, driven by `make test-integration` via `docker-compose.test.yml`. It's the one package where the in-process unit tests can't prove the thing that actually matters, that state is genuinely shared across two independently-constructed `Store` instances, standing in for two Formelay replicas.
- `internal/api`'s handler tests drive the real submission pipeline end to end (`httptest`, a fake rate limiter, a real `app.App`) rather than mocking individual steps, so they catch pipeline-ordering bugs a narrower unit test would miss.

## Extending Formelay

The channel, CAPTCHA, and spam-classifier subsystems are each a small `Registry` (`type: string` to Go constructor) plus a shared interface, following the same shape:

- **A new delivery channel**: implement `notify.Notifier` (`Send(ctx, RenderedMessage) error`, `Type() string`) in a new `internal/notify/<name>` package, and optionally `notify.TemplateProvider` if it needs templates rendered for it. Register it in `cmd/formelay/main.go` alongside `email`/`discord`/`webhook`. For anything that's just "POST JSON to an incoming webhook", you likely don't need this at all, the built-in `webhook` channel already covers Slack/Telegram/PagerDuty/etc. with zero code (see [examples.md](examples.md)).
- **A new CAPTCHA provider**: most providers speak the same reCAPTCHA-derived protocol as Turnstile/hCaptcha/reCAPTCHA, so `provider: generic` with the right `verify_url`/param names usually needs no code at all, see [Extending with a new CAPTCHA provider](examples.md#extending-with-a-new-captcha-provider). A provider with a genuinely different protocol needs a real `captcha.Verifier` implementation (`internal/captcha/generic.go` has the interface).
- **A new spam-filter provider**: implement `spamfilter.Classifier` (`Classify(ctx, render.SubmissionData) (Verdict, error)`, `Type() string`) in a new `internal/spamfilter/<name>` package and register it the same way as a channel.

In all three cases, config decoding for a new implementation is a YAML round-trip (see `internal/yamlutil.Decode`), not a new code path in `internal/config`, that package stays generic across every implementation.
