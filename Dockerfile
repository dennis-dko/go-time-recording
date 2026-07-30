# Build a single self-contained binary: the web UI is embedded via go:embed
# and the SQLite driver is pure Go, so CGO is not needed and the result runs
# on a scratch-like base.
FROM golang:1.26.5 AS build

ARG VERSION=dev

WORKDIR /src

# Copy the module files first so dependency download stays cached while only
# application sources change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 makes the binary static; the previous "-linkmode external
# -extldflags -static" needed a C toolchain and is unnecessary without cgo.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/go-time-recording ./cmd/main.go

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

WORKDIR /app

COPY --from=build /out/go-time-recording /app/go-time-recording
# GoFr reads its configuration from ./configs relative to the working directory.
COPY --from=build /src/cmd/configs /app/configs

# The SQLite file lives on a volume so data survives a container replacement.
# DB_NAME is a path here because GoFr derives the DSN as "file:<DB_NAME>.db".
RUN mkdir -p /data && chown -R app:app /data /app
VOLUME /data
ENV DB_DIALECT=sqlite \
    DB_NAME=/data/go-time-recording \
    HTTP_PORT=8000 \
    METRICS_PORT=2121

USER app

# 8000 serves the API and the UI; 2121 serves Prometheus metrics.
EXPOSE 8000 2121

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8000/.well-known/alive >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/go-time-recording"]
