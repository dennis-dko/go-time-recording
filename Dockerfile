# Build a single self-contained binary: the web UI is embedded via go:embed
# and the SQLite driver is pure Go, so CGO is not needed and the result runs
# on a scratch-like base.
FROM --platform=$BUILDPLATFORM golang:1.26.6 AS build

ARG VERSION=dev

# Which machine the image is being built for, which is not the one building it
# when more than one platform is asked for. BuildKit sets both for every target.
#
# --platform=$BUILDPLATFORM above pins this stage to the builder's own
# architecture, so the compiler runs natively and cross-compiles. Without it,
# BuildKit runs the whole stage under emulation for every foreign target, which
# turns a Go build into a quarter of an hour. There is no cgo anywhere in this
# tree, so GOOS and GOARCH are all it takes - the same reason the release builds
# its binaries for four platforms from one runner.
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Copy the module files first so dependency download stays cached while only
# application sources change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 makes the binary static; the previous "-linkmode external
# -extldflags -static" needed a C toolchain and is unnecessary without cgo.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/go-time-recording ./cmd/main.go

FROM alpine:3.24

# The version again: ARG does not cross a FROM, and the labels below want it.
ARG VERSION=dev

# What links this image to its repository.
#
# GHCR reads org.opencontainers.image.source and, when it names a repository on
# the same account, attaches the package to it: the repository grows a Packages
# section, the package page gains a link back to the source, and tooling that
# wants to know where an image came from can find out. Without it the package
# exists but is an orphan, reachable only through the account's Packages tab.
#
# The rest is the standard set a registry and a `docker inspect` will show.
LABEL org.opencontainers.image.source="https://github.com/dennis-dko/go-time-recording" \
      org.opencontainers.image.url="https://github.com/dennis-dko/go-time-recording" \
      org.opencontainers.image.documentation="https://github.com/dennis-dko/go-time-recording#readme" \
      org.opencontainers.image.title="go-time-recording" \
      org.opencontainers.image.description="Project time tracking as a single self-contained binary: REST API, embedded web interface, SQLite, PostgreSQL or MySQL." \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

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

# DB_DIALECT is deliberately absent, so this image serves its installer rather
# than choosing a database for whoever ran it.
#
# It used to be sqlite here, which quietly defeated removing the default from
# configs/.env: the environment wins, so the image was the one layer that still
# picked for you. Somebody would run it, configure an installation on a SQLite
# file inside the container, and find out which database they had been using only
# when they tried to move to a real one - which is the failure the installer
# exists to prevent.
#
# DB_NAME configures nothing on its own, and stays for what it does to the
# installer: configs/.env leaves it empty, an environment variable beats that
# file, so this is the path already filled in when somebody picks SQLite. That
# is what puts the database on the /data volume instead of inside the container,
# where replacing the container would throw it away with it.
#
# HTTP_PORT and METRICS_PORT used to be here too, repeating configs/.env - which
# is copied into this image above, so it was the same value written twice. Being
# real environment variables they also beat a configs directory mounted over the
# image's own: an operator who set HTTP_PORT in their own file was overridden by
# the image, with nothing to say so. The ports come from configs/.env now, and
# `docker run -e HTTP_PORT=...` still wins over that.
#
# Set DB_DIALECT to skip the installer, which is what deploy/compose.yaml does.
ENV DB_NAME=/data/go-time-recording

USER app

# 8000 serves the API and the UI; 2121 serves Prometheus metrics.
EXPOSE 8000 2121

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8000/.well-known/alive >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/go-time-recording"]
