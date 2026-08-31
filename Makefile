GO_IMAGE := golang:1.27-bookworm
GOMOD_VOLUME := formelay-gomod
DOCKER_RUN := docker run --rm -v $(CURDIR):/src -w /src -v $(GOMOD_VOLUME):/go/pkg/mod -e CGO_ENABLED=0 -e GOFLAGS=-mod=mod $(GO_IMAGE)

.PHONY: tidy fmt fmt-check build vet test coverage race vulncheck deadcode test-integration test-live docker-build compose-up compose-down release-snapshot

tidy:
	$(DOCKER_RUN) go mod tidy

fmt:
	$(DOCKER_RUN) gofmt -w .

fmt-check:
	$(DOCKER_RUN) sh -c 'test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)'

build:
	$(DOCKER_RUN) go build ./...

vet:
	$(DOCKER_RUN) go vet ./...

test:
	$(DOCKER_RUN) go test ./...

coverage:
	$(DOCKER_RUN) sh -c 'go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1'

race:
	docker run --rm -v $(CURDIR):/src -w /src -v $(GOMOD_VOLUME):/go/pkg/mod -e CGO_ENABLED=1 -e GOFLAGS=-mod=mod $(GO_IMAGE) go test -race ./...

vulncheck:
	$(DOCKER_RUN) sh -c 'go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...'

# Whole-program reachability analysis from the real cmd/formelay entrypoint,
# including test-only reachability (-test).
deadcode:
	$(DOCKER_RUN) go run golang.org/x/tools/cmd/deadcode@latest -test ./cmd/formelay

# Full Valkey integration suite, run against a real Valkey instance via
# docker-compose (see docker-compose.test.yml) — no local Valkey needed.
test-integration:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from integration-test
	docker compose -f docker-compose.test.yml down -v

# CAPTCHA provider tests against real Turnstile/hCaptcha verify endpoints,
# using each provider's official public test key pairs (no account/secret
# of your own needed, no browser/widget involved). Requires internet access.
test-live:
	$(DOCKER_RUN) go test -tags=live ./internal/captcha/... -v

keygen:
	$(DOCKER_RUN) go run cmd/formelay/main.go keygen

docker-build:
	docker build -t formelay:dev .

compose-up:
	docker compose up --build

compose-down:
	docker compose down

# Local dry-run of the release pipeline (no publishing) — requires
# goreleaser on the host, or run it via its own Docker image:
#   docker run --rm -v $(CURDIR):/src -w /src goreleaser/goreleaser:latest release --snapshot --clean
release-snapshot:
	docker run --rm -v $(CURDIR):/src -w /src goreleaser/goreleaser:latest release --snapshot --clean
