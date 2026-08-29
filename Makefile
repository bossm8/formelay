GO_IMAGE := golang:1.27-bookworm
GOMOD_VOLUME := formelay-gomod
DOCKER_RUN := docker run --rm -v $(CURDIR):/src -w /src -v $(GOMOD_VOLUME):/go/pkg/mod -e CGO_ENABLED=0 -e GOFLAGS=-mod=mod $(GO_IMAGE)

.PHONY: tidy fmt fmt-check build vet test race vulncheck test-integration docker-build compose-up compose-down release-snapshot

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

race:
	docker run --rm -v $(CURDIR):/src -w /src -v $(GOMOD_VOLUME):/go/pkg/mod -e CGO_ENABLED=1 -e GOFLAGS=-mod=mod $(GO_IMAGE) go test -race ./...

vulncheck:
	$(DOCKER_RUN) sh -c 'go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...'

# Full Valkey integration suite, run against a real Valkey instance via
# docker-compose (see docker-compose.test.yml) — no local Valkey needed.
test-integration:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from integration-test
	docker compose -f docker-compose.test.yml down -v

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
