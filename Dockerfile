FROM golang:1.27-bookworm AS build

ARG VERSION=dev
ARG COMMIT=none

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/bossm8/formelay/internal/version.Version=${VERSION} -X github.com/bossm8/formelay/internal/version.Commit=${COMMIT}" \
    -o /out/formelay ./cmd/formelay

# --------
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
ARG COMMIT=none
ARG CREATED=unknown

COPY --from=build /out/formelay /usr/local/bin/formelay

LABEL org.opencontainers.image.title="formelay" \
      org.opencontainers.image.description="Receive website form submissions and relay them to email, Discord, or a webhook." \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.source="https://github.com/bossm8/formelay" \
      org.opencontainers.image.url="https://github.com/bossm8/formelay" \
      org.opencontainers.image.documentation="https://github.com/bossm8/formelay/blob/main/README.md"
# No org.opencontainers.image.licenses yet: this repo has no LICENSE file/
# SPDX identifier chosen. Add one here (and above, for the release image)
# once it does.

USER nonroot:nonroot

EXPOSE 8080 9090

HEALTHCHECK --interval=15s --timeout=3s --retries=3 \
    CMD ["/usr/local/bin/formelay", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/formelay"]
CMD ["--config", "/etc/formelay/config.yaml"]
