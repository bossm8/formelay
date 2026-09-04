# AGENTS.md

Standing brief for any coding agent (Claude Code or otherwise) working on
**formelay** (`github.com/bossm8/formelay`, Go 1.27). This file captures
review standards and working conventions established over many prior
sessions — read it before making changes. For project layout, `make`
targets, testing setup, and how to add a new channel/CAPTCHA
provider/spam classifier, see [docs/develop.md](docs/develop.md); for
config reference and the security model, see
[docs/configuration.md](docs/configuration.md) and the README's
[Security model](README.md#security-model) section. This file does not
repeat any of that — it only adds what those documents don't cover.

There is no CONTRIBUTING.md or PR/issue template in this repo; this file
is the whole brief.

## No local Go toolchain — everything runs via Docker

There is no Go install on the host, by design. Every `go` command (and
`gofmt`, `govulncheck`, `deadcode`, ...) runs inside `golang:1.27-bookworm`
via Docker, wrapped by the `make` targets in `Makefile` (`DOCKER_RUN`,
module cache in the named volume `formelay-gomod`). **Use the `make`
targets** (`build`, `vet`, `test`, `race`, `fmt`, `fmt-check`, `tidy`,
`coverage`, `vulncheck`, `deadcode`, `test-integration`, `test-live`)
rather than hand-crafting `docker run ... go ...` invocations — `make
race` in particular already sets `CGO_ENABLED=1` (the default
`DOCKER_RUN` uses `0`, since the rest of the build is
`CGO_ENABLED=0`/static), so there's no need to reconstruct that
override yourself. Live end-to-end verification (see "Verify live, not
just with unit tests" below) similarly goes through `make docker-build`
+ `make compose-up`/`docker compose`, never a bare local binary. See
[docs/develop.md](docs/develop.md) for the full target list and what CI
runs on every push/PR.

## No dead code

Every documented config field, every registered metric, every exported helper,
every struct field, every interface method must be **actually exercised** by a
real code path. Nothing "reserved for future use." Nothing that merely looks
wired up.

Before adding, keeping, or removing anything, verify with an actual
audit — grep every usage site yourself. Do not trust a tool's silence as
proof something is used, and do not trust your own or another agent's
claim that something is dead without checking every call site first.

- `golang.org/x/tools/cmd/deadcode` (run against the real entrypoint,
  with `-test`) is the authoritative signal for whole unreachable
  functions.
- `staticcheck -checks U1000` is a weaker secondary signal (conservative
  about exported identifiers, so it under-reports).
- Neither tool catches dead struct fields, dead interface methods, or
  docs describing behavior that doesn't exist — those need manual
  grep-verification, one usage site at a time.

When you find something unused, there are exactly two valid outcomes:
**remove it** (if it's genuinely redundant with something that already
does the job), or **wire it up for real** (if it represents intended
functionality that just never got connected). Leaving it half-done, or
adding a comment excusing it, is not an option.

This has happened for real, multiple times, in this project's history —
useful precedent for what "done" looks like (verify each still holds
before citing it, the code moves):

- **Removed as pure dead config**: `security.default_allowed_origins`,
  `logging.audit.format` (the audit log is always structured JSON,
  regardless of `logging.format` — see the comment on `AuditConfig` in
  `internal/config/types.go`).
- **Wired up because it was genuine but disconnected functionality**:
  `logging.level` / `logging.format` (drive `slog` handler construction
  in `cmd/formelay/main.go`'s `buildLogger`), `logging.audit.enabled` /
  `logging.audit.log_field_values` (gate `audit.Logger.Log`),
  `server.tls.*` (drives `ListenAndServeTLS` in `cmd/formelay/main.go`
  and is validated in `internal/config/validate.go`), five Prometheus
  metrics that were registered but never populated
  (`formelay_config_reload_total`,
  `formelay_config_last_reload_timestamp_seconds`,
  `formelay_ratelimit_buckets_active`,
  `formelay_ratelimit_backend_errors_total`,
  `formelay_http_requests_in_flight`), `email.Config.ReplyToField` (was
  decoded but never read — Reply-To was silently hardcoded to one field
  regardless of config; fixed via the new
  `notify.ReplyToFieldProvider` optional interface in
  `internal/notify/notifier.go`), `sanitize.HeaderSafeAddress` (existed
  but was never called — a duplicate inline check was used instead;
  now wired into `internal/notify/email/email.go`).
- **Removed as genuinely redundant**: `captcha.Verifier.Type()` /
  `spamfilter.Classifier.Type()`, `notify.RenderedMessage.FormID` /
  `.ChannelID`, a couple of dead `Meta[...]` writes.

## Never document behavior that doesn't exist

`docs/configuration.md`, `docs/metrics.md`, and `README.md` must
describe only what the code verifiably does — never what's intended,
planned, or aspirational. If you change behavior, update the docs in the
same change; if you're unsure whether docs match code, check before
trusting either.

## Don't add dependencies unilaterally

Do not add a new Go module to `go.mod`, a new GitHub Action, or a new
external service/account without asking the maintainer first and
explaining the trade-off — even if the addition looks obviously correct.

More generally: when there's a real design decision with trade-offs
("wire this up vs. remove it," "which external service handles X,"
"which of several reasonable approaches") — ask, rather than deciding
unilaterally.

## Verify live, not just with unit tests

This project has repeatedly found real bugs (a config-hot-reload bug,
the `ReplyToField` bug above) that unit tests missed because they didn't
exercise the actual reload/wiring lifecycle — the bug only showed up
when someone actually ran the built image and inspected real output.

Before calling a fix done, where practical:

- Build and run it for real (`docker compose up`, or `make
  docker-build` + `make compose-up`).
- Hit it with real `curl` requests.
- Inspect real logs/metrics output, not just reasoning that the code
  looks right.

Clean up afterward: `docker compose down -v`, and restore any example
config/`.env` files you touched to their committed placeholder state, so
the repo is left exactly as clean as it started.

## Security review standard

No OWASP-class vulnerabilities (injection, XSS, header injection,
etc.). This project's specific posture — verify each still holds in
`internal/sanitize`, `internal/notify/email`, and the README's Security
model section before restating it as fact:

- `auth.site_key` is a **public** capability token (like a reCAPTCHA
  site key), not a secret. The real defense is layered — origin
  allowlist, rate limiting, honeypot, CAPTCHA, AI spam filter — not the
  token's secrecy.
- XSS defense is contextual output escaping at render time
  (`html/template` auto-escaping, plus an explicit `json` template func
  for `text/template` contexts), not input allowlisting. Sanitization
  (`bluemonday` + Unicode NFC normalization, in `internal/sanitize`) is
  defense-in-depth, not the primary defense.
  `internal/spamfilter/ai` field allowlisting is a data-minimization
  control (PII need not reach a third party), not a security boundary.
- Header-injection-style attacks (e.g. a crafted Reply-To) are prevented
  by rejecting malformed input outright
  (`sanitize.HeaderSafeAddress` → `net/mail.ParseAddress`), not by
  stripping characters.

## Testing conventions

- Table-driven tests live next to the code they cover (`foo.go` +
  `foo_test.go`).
- Tests against real external services/infrastructure are excluded from
  the default `go test ./...` via a build tag, and get their own `make`
  target:
  - `internal/captcha/live_test.go` — `//go:build live`, run via `make
    test-live`. Hits the real Turnstile/hCaptcha verify endpoints with
    each provider's official public test key pairs (no account/secret
    needed).
  - `internal/ratelimit/valkey/integration_test.go` — `//go:build
    integration`, run via `make test-integration`. Needs a real Valkey,
    started through `docker-compose.test.yml`; proves state is actually
    shared across two independently-constructed `Store` instances
    (standing in for two formelay replicas), which an in-process unit
    test can't prove.
- `internal/api`'s handler tests drive the real submission pipeline
  end-to-end (`httptest`, a fake rate limiter, a real `app.App`) rather
  than mocking individual steps, to catch pipeline-ordering bugs a
  narrower unit test would miss. Follow that pattern for new pipeline
  behavior rather than mocking deeper.

See [docs/develop.md](docs/develop.md#testing-approach) for the full
list of `make` targets and what CI runs on every push/PR.

## Code style

- No premature abstraction — three similar lines beat a speculative
  helper.
- No defensive code for scenarios that structurally can't happen.
- Comments explain *why* (a non-obvious constraint, invariant, or
  gotcha), never *what* (the code already says that).
- Prefer editing an existing file over creating a new one.
- No half-finished implementations — see "no dead code" above.

## Commit discipline

- Don't - Don't perform git actions, maintainers will do this manually
