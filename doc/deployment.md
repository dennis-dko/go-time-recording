# Releases and deployment

> Operating this rather than developing it?
> [`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md) is the manual: the four ways to
> run it, what to back up, what needs a restart, what the health endpoints
> actually mean, and the handful of behaviours that surprise people. This page
> is about how a release is produced, and how one reaches a server.

**Every merge to `main` is a release.** The patch number goes up, the image is
published to GHCR as both that version and `:latest`, and a GitHub release is
created with generated notes. The version is not written down anywhere:
[`release.yml`](../.github/workflows/release.yml) reads the newest tag and counts on,
so there is no file to forget to bump and no way for a tag and a constant to
disagree. The patch counts to 99 and then hands on to the minor — after `v0.2.99`
comes `v0.3.0` — and the minor does the same to the major
([`build/nextversion`](../build/nextversion/main.go) holds the rule). With no tags at
all it starts at `v0.1.0`.

The image is built into the runner's own daemon first, started there with nothing
but `DB_DIALECT`, and asked for `/api/v1/branding` — the body rather than the
status, because the installer answers every path with a 200 and only the
application puts a version in the answer. It is pushed from that same daemon
afterwards, so the bytes somebody pulls are the bytes that answered. Nothing is
published if they do not: this used to run after the push and end in a warning,
which made it a note in a log rather than a gate.

For a minor or major bump, tag it by hand: `task release VERSION=v1.2.0` pushes
the tag and that exact version is released — the next merge then counts on from
there.

**What a release carries.** The image on GHCR — for `linux/amd64` and
`linux/arm64`, so the same `docker compose up` works on a server and on a
Raspberry Pi — and a binary for each of

| | |
| --- | --- |
| `linux_amd64` | the usual server |
| `linux_arm64` | a Raspberry Pi or an ARM instance |
| `windows_amd64.exe` | a desktop or a Windows server |
| `darwin_arm64` | an Apple Silicon Mac |

plus `SHA256SUMS` to check a download against. They are one file each: the
interface, the timezone database and the migrations are all compiled in, so
downloading one and running it is a complete installation — it serves the
installer until it has a database.

What the installer cannot ask for — the port, TLS, the log level, the session
lifetime, the rate limit — comes from the environment or from `configs/.env`
beside the binary.
[`deploy/.env.binary.example`](../deploy/.env.binary.example) lists every variable
the process reads, with the value it uses when the variable is absent; the release
notes point at it too, because that is the moment somebody needs it. Settings the
interface can change live in the database and are not read from there.

The two example files are not interchangeable, which is worth saying because the
release notes used to send binary downloaders to the wrong one.
[`deploy/.env.example`](../deploy/.env.example) is read by docker compose and carries
only what the compose files interpolate — database credentials, the image tag —
several of which mean nothing beside a single binary.

Cross-compiled from a single runner, which works because there is no cgo anywhere
in the tree: the SQLite driver is modernc's pure-Go one, so `GOOS` and `GOARCH`
are all it takes. The same `-ldflags` as the image, so a downloaded binary reports
the same version in its footer as the container of that release does.

The merge path waits for CI rather than verifying again. CI runs the integration
suite against SQLite, PostgreSQL and MySQL on every push to `main`, so repeating a
subset of it would add twenty minutes to every merge to discover nothing — and it
means only a commit that went green is ever published. The tag path verifies for
itself, because CI does not run on tags.

Two details worth knowing if you change that file. The tag is created by the
release action rather than by a `git push`, because a tag pushed with
`GITHUB_TOKEN` triggers no further workflows — a "tag now, release on the tag"
arrangement would tag and then sit there. And the commit that gets released is the
one CI verified, not whatever `main` points at by the time the run finishes.

Nothing has to be built on the server.

## On the server

Copy [`deploy/`](../deploy/) — three compose files, two environment templates and
[the manual](../deploy/OPERATIONS.md) — and nothing else. The source tree is not
needed.

```bash
cp .env.example .env
$EDITOR .env            # DB_PASSWORD is required and has no default
chmod 600 .env
docker compose up -d
```

That brings up the published image and a PostgreSQL beside it, both with
`restart: unless-stopped` and their data on named volumes. The application
listens on `127.0.0.1:8000`, for a reverse proxy in front of it. PostgreSQL
publishes **no port at all**: it is reachable from the application container by
name, and from nowhere else.

Pin `GTR_VERSION` to a release in your own `.env`. A container restarted at 3am
otherwise comes back as a different version than the one that went down.

`deploy/.env.example` deliberately leaves it on `latest`, which is the opposite
advice for the opposite situation: a version written into an example ages with
every release and nothing moves it. That one sat at `v0.1.19` while the newest
release was `v0.1.72`, so following the documented setup installed a version from
fifty-three releases earlier. A test holds the example at `latest`; pinning is
for the file you write.

The compose file sets `DB_DIALECT`, so a server deployment never meets the
installer — it is configured before it starts, which is what an unattended
deployment needs. Leaving `DB_DIALECT` out is what turns the installer on, and a
container waiting for somebody to click something is rarely what you want on a
server; if you do, set `SETUP_TOKEN` so the token is not something you have to go
and read out of `docker logs`.

## HTTPS

With a reverse proxy already terminating TLS, three things are on you, and none
of them fails in a way that says what is wrong: it has to pass
`X-Forwarded-Proto: https` (nothing else tells this process the browser is on
HTTPS, and without it the session cookie is written without `Secure` and no HSTS
is sent), the client's address has to survive the hop or the sign-in rate limit
counts every visitor as one caller, and the plain port has to be closed to
everything but the proxy — the guard that refuses network traffic there only runs
while *this* process is serving HTTPS, which behind a proxy it is not.
`compose.yaml` already publishes to the loopback interface, so the last of those
is done for you; the single binary is not.

It also needs a hostname of its own rather than a path under one: the interface
asks for `/app.js` and `/api/v1` absolutely, and nothing moves them.
`deploy/OPERATIONS.md` has the whole Apache configuration and what each line is
for.

To terminate TLS in the application instead, add the overlay:

```bash
docker compose -f compose.yaml -f compose.tls.yaml up -d
```

Certificates come from Let's Encrypt automatically, and are kept on a volume so
a restart does not request them again — their rate limit is five per domain per
week, and running into it leaves the site with no certificate at all. Two
things must be true, and neither is in the compose file: `TLS_DOMAINS` has to
resolve to this server publicly, and port 80 has to be reachable, because that
is where the challenge arrives.

Do not terminate TLS in both places. Two certificates for one name means one
renewal that quietly stops working.

## Tracing

A third overlay runs a Jaeger beside the application. It only adds a service, so
it combines with the TLS one in either order:

```bash
docker compose -f compose.yaml -f compose.tracing.yaml up -d
```

It supplies the collector and configures nothing on the application, because
tracing is administered in the running application and what is stored there is
applied over the environment at the next start — a variable set in the compose
file would be the losing half of a disagreement nobody can see. Switch it on
under *Settings → Logging, metrics and tracing*: exporter `OTLP`, collector
`jaeger:4317` (the service name; `http://` in front means the same, and
`https://` would encrypt the export to a collector that serves TLS, which this
one does not), the recorded share at `1` while investigating something. **Then restart**: the
exporter is built while the application starts, so a saved setting does nothing
until it does. The banner across the top of the screen says so and offers the
button.

The trace browser is on `127.0.0.1:16686`, published to the loopback interface
only — it asks nobody to sign in, and traces carry request paths and the
identifiers in them. Reach it with `ssh -L 16686:127.0.0.1:16686 <server>`.
[`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md) has the rest, including why the
traces are held in memory and what that costs.

## A directory, if you have none

A fourth overlay runs an OpenLDAP beside the application:

```bash
docker compose -f compose.yaml -f compose.ldap.yaml up -d
```

**Most installations should not use it.** This application is a directory
client: if there is already an Active Directory or an OpenLDAP, you point at it
under *Settings* and run no service here — and an installation whose people
already sign in with a password, a passkey or an authenticator code needs no
directory at all. Adding one is a second stateful service to back up and a
second password store to keep, for the same people. It is worth it for two
cases: seeing what the feature does before pointing it at the real directory,
and genuinely having no directory and wanting one.

Like the tracing overlay it sets nothing on the application — the connection is
administered under *Settings*, and the header of
[`compose.ldap.yaml`](../deploy/compose.ldap.yaml) lists every field to fill in,
including why the id attribute must be `entryUUID` and not the mail address.

It comes up empty on purpose: a suffix, an `ou=people` to put staff in, and the
read-only account the application binds as. Invented people would arrive in the
time recording on the first synchronisation, named after nobody. The header
shows the `ldapadd` for a real one.

Unlike the other three files this one needs the source beside it: it builds its
image from [`deploy/ldap/`](../deploy/ldap/), because the OpenLDAP images most
compose files reach for are withdrawn or archived upstream, and this container
holds the credentials everybody signs in with.

That is the image the test environment runs too, handed a throwaway password and
a seed of five invented people — the same arrangement the test PostgreSQL and
MySQL get, which is the real image with a weak password. It was a near-copy for
a while, and the copy cost the same forty lines of directory configuration in
two places while meaning that every bind the suite exercised was exercised
against something no deployment ever starts.

## Updating

```bash
docker compose pull && docker compose up -d
```

Schema migrations run at start-up, so there is no separate step. Back the
database up first — the application will not undo a migration for you:

```bash
docker compose exec -T postgres pg_dump -U "$DB_USER" "$DB_NAME" | gzip > backup-$(date +%F).sql.gz
```

## Configuration

`.env` carries only what a deployment must decide: the database password, the
image version, the instance name, the TLS settings, and — only with the
directory overlay — the two passwords that directory comes up on. Everything
operational — session lifetime, rate limits, the daily booking cap, the
directory *connection*, the timezone — is administered in the running
application under *Settings*, and takes effect without a restart. See
[Configuration](configuration.md) for which settings live where and why.

## Running it without containers

The binary is self-contained; the image is a convenience, not a requirement.

```bash
DB_DIALECT=postgres DB_NAME=… DB_HOST=… DB_USER=… DB_PASSWORD=… ./go-time-recording-web
```

`DB_NAME` is not optional and this line used to leave it out, which does not
start: the connection is validated before anything else happens, and a server
dialect without a name is refused with that as the reason. The port may be left
out — the application fills in the dialect's own.

It does not need a `configs/` directory; a missing one is tolerated and every
value has a default. What the **working directory** decides is where
`configs/datasource.json` is read and written, where a relative SQLite file
lands, and where the default TLS cache `configs/certs` goes — so start it from
the same place every time. [`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md) has a
systemd unit that gets that right.
