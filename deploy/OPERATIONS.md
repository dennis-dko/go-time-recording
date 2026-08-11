# Running this

For whoever deploys and operates the application. It assumes nothing about the
source tree — everything here needs the files in this folder and a downloaded
artifact. If you are changing the code, [`../README.md`](../README.md) is the
longer document and explains why things are the way they are.

## What you are deploying

One file. The web interface, the timezone database and the schema migrations are
compiled in, and the SQLite driver is pure Go, so there is no asset directory, no
Node build and no runtime to install beside it. It provisions its own database
schema on first start: there is no separate migration step to run, before or
after an upgrade.

Two listeners:

| Port | Serves | Exposure |
| --- | --- | --- |
| `8000` | the API and the web interface | behind TLS, always |
| `2121` | Prometheus metrics **and Go's `/debug/pprof/`** | your monitoring only, never the internet |

The second one is outside the middleware chain. It asks for no sign-in and is not
covered by TLS, so anywhere it is reachable, so is a heap dump. Bind it to a
private interface, firewall it, or switch it off under *Settings*.

## Choose a deployment

| | Command | Database | Listens on |
| --- | --- | --- | --- |
| **A** Compose | `docker compose up -d` | PostgreSQL, in the stack | `127.0.0.1:8000` |
| **B** Compose + TLS | `docker compose -f compose.yaml -f compose.tls.yaml up -d` | PostgreSQL, in the stack | `:80` and `:443` |
| **C** Single binary | `./go-time-recording` | your choice, asked on first start | `:8000` |
| **D** Bare container | `docker run …` | asked on first start | `:8000` |

A is the normal answer. B if nothing else terminates TLS. C for a small
installation without Docker, or inside systemd. D mostly for a look.

---

## A · Compose

Copy this folder to the server. The source tree is not needed.

```bash
cp .env.example .env
$EDITOR .env            # DB_PASSWORD is required and has no default
chmod 600 .env
docker compose up -d
```

Then open `http://127.0.0.1:8000` through whatever proxy you put in front, and
sign in as `admin@local` / `changeme123`. Read *First start* below before you do.

Two things in [`compose.yaml`](compose.yaml) are deliberate and worth not
"fixing":

- **The application publishes on `127.0.0.1:8000`, not `0.0.0.0`.** Without TLS
  in front, a port on the network serves session cookies in the clear. A reverse
  proxy on the same host reaches it here; nothing else does.
- **PostgreSQL publishes nothing at all.** It is reachable from the application
  container by name. A published `5432` is how databases end up on the internet.

Because Compose sets `DB_DIALECT`, the installer is skipped — the stack comes up
straight into the application.

Pin the version rather than following `latest`, so a restart cannot become an
unplanned upgrade:

```bash
GTR_VERSION=v1.2.3
```

## B · Compose with TLS

```bash
docker compose -f compose.yaml -f compose.tls.yaml up -d
```

Set both of these in `.env` first. Compose interpolates them with the error form,
so it **refuses to start anything** while either is missing, and `.env.example`
ships them commented out:

```bash
TLS_DOMAINS=time.example.com     # comma separated for several
TLS_EMAIL=ops@example.com
```

`TLS_EMAIL` is where Let's Encrypt sends expiry warnings. It is worth a real
mailbox: that notice is the only warning that renewal has stopped working.

The overlay then obtains certificates automatically. Two more things must be true
and neither is in any file:

- `TLS_DOMAINS` must resolve to this server **publicly**. Let's Encrypt verifies
  by connecting to it.
- Port 80 must be reachable from the internet. The challenge arrives there, and
  it also redirects visitors to HTTPS.

Certificates live on their own volume. Losing it makes every restart ask again,
against a limit of five per domain per week — after which the site has no
certificate at all.

If a proxy, load balancer or ingress already terminates TLS, leave this overlay
out. Two certificates for one name means one renewal that quietly stops working.

## C · Single binary

Download the file for your platform from the release, check it against
`SHA256SUMS`, and run it.

```bash
mkdir -p /opt/gtr/configs
cd /opt/gtr
cp /path/to/.env.binary.example configs/.env
$EDITOR configs/.env
chmod 600 configs/.env
./go-time-recording
```

**The working directory matters.** It is what `configs/` is resolved against, so
it decides where `configs/datasource.json` is written, where a relative SQLite
file lands, and where the default TLS cache `configs/certs` goes. Start it from
the same directory every time.

A systemd unit that gets that right:

```ini
[Unit]
Description=Time Recording
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gtr
Group=gtr
WorkingDirectory=/opt/gtr
ExecStart=/opt/gtr/go-time-recording
Restart=on-failure
RestartSec=5s

# The application answers the request before replacing itself, and waits for
# requests in flight on shutdown - SHUTDOWN_GRACE_PERIOD, 30s by default.
TimeoutStopSec=45s

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/gtr

# Only needed when this process terminates TLS itself. Without it an
# unprivileged user cannot bind 443 or 80 - see below, because the way that
# fails is the problem.
# AmbientCapabilities=CAP_NET_BIND_SERVICE
# CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

`ProtectSystem=strict` with `ReadWritePaths` is the part that matters: the
process writes `configs/datasource.json`, the SQLite file if you chose one, and
the certificate cache — all under its working directory and nowhere else.

### If this process terminates TLS

Two things to know before you switch `TLS_ENABLED=true` on a host.

**Uncomment the two capability lines.** A container gets low ports for free; a
host does not. As written, the unit runs as `gtr` with `NoNewPrivileges=true`, so
binding 443 and 80 fails with "permission denied".

**And that failure does not stop the process.** Both listeners are started in
their own goroutines and only log; startup reports success either way. So the
service comes up, `systemctl status` says `active (running)`, and the
installation serves **unencrypted** HTTP on `HTTP_PORT` with one error line in
the log. After enabling TLS, check the log for `serving HTTPS on :443` and
actually connect to the name over HTTPS. Do not take "the unit started" as
evidence.

**The plain listener stays open on all interfaces regardless.** The TLS server
proxies to it and nothing closes it. Firewall `HTTP_PORT`, or expose only 80 and
443 on the public interface.

## D · Bare container

```bash
docker run -d -p 8000:8000 -v gtr-data:/data \
  ghcr.io/dennis-dko/go-time-recording:v1.2.3
```

With no `DB_DIALECT` this serves its **installer** and waits — that is on
purpose. Setting `DB_DIALECT` skips it, which is what Compose does.

The image bakes in three variables and no more:

| | |
| --- | --- |
| `DB_NAME=/data/go-time-recording` | the path to offer if you pick SQLite, so the data lands on the volume rather than inside the container |
| `HTTP_PORT=8000` | |
| `METRICS_PORT=2121` | |

They are real environment variables, so they sit at layer 3 and beat
`configs/.env` — which is what lets `docker run -e HTTP_PORT=…` change them.

Only `/data` is persistent. Anything the process writes elsewhere — including
`configs/datasource.json`, so the answer you gave the installer — disappears with
the container unless you mount it.

---

## Where configuration comes from

Four layers. The highest one that has a value wins.

| | Source | Set by |
| --- | --- | --- |
| 1 | `configs/.env` | shipped — every default |
| 2 | `configs/.$APP_ENV.env` | shipped — differences per environment |
| 3 | real environment variables | you: Compose, systemd, the shell |
| 4 | `configs/datasource.json` | the installer, and *Settings → Database* |

Layer 3 is why a deployment needs no file edits: Compose sets everything it needs
as environment variables. Layer 4 is above it on purpose — changing the database
in the interface is an explicit act, and it would be surprising for a stale
variable to override it silently. It only ever supplies `DB_*`, and only the
fields it actually holds.

`APP_ENV` selects layer 2, and it has to come from the **real environment**: GoFr
reads it before it opens any file, so setting `APP_ENV` inside `configs/.env`
cannot select an overlay. With it unset, layer 2 falls back to
`configs/.local.env`.

**The rule that explains the rest:** anything the running application can change
lives in the **database**. For those settings an environment variable only
decides what a fresh installation starts with — it does not override an
administrator who has already changed it. What stays in the environment is what
must be settled before the process exists: the ports, TLS, the log level, and the
two switches that could remove the interface that would put them back.

### The two example files here

They are not interchangeable.

| File | Read by | For |
| --- | --- | --- |
| [`.env.example`](.env.example) | **docker compose** | deployments A and B |
| [`.env.binary.example`](.env.binary.example) | **the process** | deployment C |

`.env.example` carries only what the compose files interpolate — database
credentials, the image tag. Several of its values mean nothing beside a binary.
`.env.binary.example` lists what the process itself reads, with the value that
applies when the variable is absent.

Neither has to exist. The binary runs with no configuration file at all: with no
database configured it serves its installer, and everything else has a default.

### Values worth setting deliberately

```bash
# Stops the framework calling home. It defaults to ON - .env.binary.example
# writes that default out so it is visible rather than implied - so an air-gapped
# or egress-filtered deployment wants this line instead. Three destinations, one
# variable:
#   https://gofr.dev/api/ping/up   and  .../ping/down   at start and shutdown
#   https://gofr.dev/telemetry/v1/metrics                a startup document -
#     app name and version, framework and Go version, OS, architecture
GOFR_TELEMETRY=false

# Exactly "0" switches the metrics endpoint off. Any other unparseable value -
# "off", "false", "-1", empty - falls back to 2121 and switches it ON. Given what
# that port serves, the distinction is worth getting right.
METRICS_PORT=0
```

For `postgres` and `mysql` you may leave `DB_PORT` out: the application fills in
5432 or 3306 for the dialect and hands that to the database layer, so the port it
proved is the port it uses.

## First start

```
no database configured
        │
        ▼
INSTALLER on :8000              a token, the dialect, the connection - which is
        │                       tested before it is written to
        ▼                       configs/datasource.json, then the application
                                carries on in the same process
        ▼
sign in                         admin@local / changeme123
        ▼
SETUP WIZARD                    1 change that password   (required)
        │                       2 pick the timezone      (required)
        │                       3 name the installation  (optional)
        ▼                       4 connect a directory    (optional)
create the first user           Name, email, role, password
        ▼
that person's first sign-in     lands on the welcome screen; the guided tour
                                starts by itself
```

**The setup token.** There is no account to authenticate against yet, so the
installer requires a token. It is generated and printed in the process log at
start, with the URL to open. For an unattended install set `SETUP_TOKEN`
yourself; then nothing is printed and a provisioning script can drive the page.

Two things about an exposed installer: `GET /install/state` needs no token and
discloses the instance name, the build version and whatever database prefill the
environment supplied — dialect, name, host, port, user, SSL mode. No password
leaks, but the intended topology does. Do not leave the port open to the world
while it is sitting there.

**Change the initial password before anything else.** Until it is changed, that
account cannot issue API tokens — but it *can* reach the whole *Settings*
surface: the database connection, the directory bind, telemetry, the process log,
the restart. `changeme123` is effectively full control of the installation, not a
limited foothold.

The built-in administrator **records no time**. It sets up the installation and
manages accounts and roles; it has no projects, no entries, no overtime balance
and no reports, and the server refuses those to it by the same code that refuses
them to anybody else. Somebody who both works here and administers gets the
combined role instead of a second account.

## Settings that need a restart

Most changes apply within seconds. These do not, and appear in *Settings* as a
list of pending changes with the running value beside the stored one:

| Setting | Why | Shown as pending? |
| --- | --- | --- |
| the database connection | never swapped under live requests | **only if the dialect changed** |
| log level | the logger's level is read at start | yes |
| metrics off | the port is bound at start | yes |
| trace exporter, collector URL | the exporter is built at start | yes |
| trace sample ratio | same exporter | **no** |
| the directory sync schedule | a cron job is registered at start | yes |

Applying immediately: the operational limits, the whole directory connection, the
instance timezone, branding and the logo, maintenance mode, users and roles.

**The pending list is not a complete account of what is waiting.** Two changes
need a restart and appear nowhere in it:

- **A database change that keeps the dialect.** Moving to another host, port, user
  or password is compared only by dialect, so `postgres` → `postgres` shows
  nothing pending. Reporting more would mean reading a stored password back to
  compare it. The `restart required` in the answer when you save is the signal.
- **The trace sample ratio.** It is exported to the tracer at start like the rest
  of the exporter settings, but it is not among the values compared.

If you changed either and the list is empty, the change is still waiting.

**The in-application restart button replaces the process image, and passes the
current environment on.** That matters in one case: a setting you cleared back to
*follow the configuration file* is **not** restored by it, because the variable
the previous process exported is inherited and still beats the file. The same
goes for deleting `configs/datasource.json` — the inherited `DB_DIALECT` keeps
the old connection instead of bringing the installer back. Those need a real stop
and start.

On Windows the button is not offered at all: there is no `execve`, so the nearest
equivalent would leave a window with no application running. The card says so.

## Monitoring

| Endpoint | Port | Meaning |
| --- | --- | --- |
| `/.well-known/alive` | application | the process is up |
| `/.well-known/health` | application | see below |
| `/metrics` | 2121 | Prometheus |

Two caveats that decide whether your alerting is worth anything:

- **`/.well-known/health` answers `200` even when the database is down.** The
  state is in the body, as `"status": "UP"` or `"DEGRADED"`. Alert on the body,
  not the status code.
- **`/.well-known/alive` answers `200` while the installer is running**, because
  the installer serves its page for every path. So the container healthcheck
  reports healthy for a container that is sitting on the installer with no
  application behind it. If you watch one thing, watch `/api/v1/branding` for a
  `version` field — that is the application and nothing else.

A metric is published only once it has a value. An installation that has had no
refused sign-in publishes no `gtr_signin_failures_total` at all, rather than
publishing zero. Treat an absent series as absent — `absent()` — not as a healthy
zero.

## Backup

There is **no backup feature in the application**. No button, no endpoint, no
permission. It is external, and what to copy depends on the dialect.

**PostgreSQL or MySQL** — everything is in that server: accounts, roles and their
permissions, projects, time entries, running timers, sessions, API tokens,
passkeys, and the whole settings table, which includes branding and the logo, the
instance timezone, the directory configuration *including its bind password*,
telemetry and the operational limits. A server-side dump is the backup.

```bash
docker compose exec -T postgres pg_dump -U "$DB_USER" "$DB_NAME" | gzip > gtr-$(date +%F).sql.gz
```

**SQLite** — the file is `<DB_NAME>.db`, with a trailing `.db` in `DB_NAME`
trimmed first. In the image that is `/data/go-time-recording.db` on the `/data`
volume; for a binary it is relative to the working directory. The application
runs SQLite in write-ahead logging mode, so a copy taken while the process runs
must include the `-wal` and `-shm` sidecars, or use SQLite's own backup:

```bash
sqlite3 /data/go-time-recording.db ".backup '/backup/gtr-$(date +%F).db'"
```

**Two paths matter whatever the dialect:**

- `configs/datasource.json` — the connection, database password included. It is
  what decides where everything else lives.
- `TLS_CACHE_DIR` — `configs/certs` by default, `/certs` under the TLS overlay.
  Losing it means asking Let's Encrypt again, against their weekly limit.

Add `configs/.env` and any `APP_ENV` overlay if the deployment uses one.

**Dump before an upgrade.** Migrations run at start-up, and they run forward
only.

## Upgrading

```bash
# A · B
GTR_VERSION=v1.3.0 docker compose up -d      # after a dump

# C
systemctl stop gtr && cp go-time-recording_v1.3.0_linux_amd64 /opt/gtr/go-time-recording && systemctl start gtr
```

The schema migrates itself on the way up. There is no separate command, and
nothing to run afterwards.

## Directory (LDAP) synchronisation

The connection is configured **in the application**, under *Settings*, not in the
environment — a second place to write it would only disagree with the first. Only
two variables exist here: `LDAP_SYNC_SCHEDULE` (empty, so no scheduled run) and
`LDAP_SYNC_MAX_DELETE_RATIO` (`0.5`).

A run reads the directory and never writes to it. It creates accounts the
directory holds and this installation lacks, and it **deletes** directory-backed
accounts the directory no longer holds — a purge, in one transaction: running
timers, time entries, that person's projects, API tokens, passkeys, sessions,
then the account. It is irreversible and it takes the recorded hours with it.
Local accounts and the built-in administrator are never touched.

Three guards stand between a misconfigured filter and a mass deletion:

1. A failed directory read is an error, never "the directory is empty".
2. A directory that returns zero users aborts the run rather than deleting
   everyone.
3. `LDAP_SYNC_MAX_DELETE_RATIO` refuses any run that would remove more than that
   share of the directory-backed accounts, and names the counts. `0` disables the
   check.

**Preview before you run.** The dry run reports the same candidates and, for each
one, how many time entries would be destroyed.

Expired sessions are pruned at 03:00 daily. That schedule is not configurable.

## Special modes

```bash
UI_ENABLED=false     # REST API only, no web interface
AUTH_ENABLED=false   # EVERY caller has every right
```

Both are read once at start and are deliberately not administrable: either one
switched off removes the interface that would switch it back. `AUTH_ENABLED=false`
suits a throwaway local trial and nothing else — it logs a warning on every
start, and the interface writes "Authentication disabled" in its header so nobody
mistakes it for a configured installation.

## Things that will surprise you

- Parsing is forgiving, not strict. A duration, integer or float that does not
  parse becomes the default **silently**. `LOG_LEVEL` resolves any unrecognised
  name to `INFO`. A `TRACER_RATIO` it cannot read samples nothing.
- `TLS_ENABLED=true` with an empty `TLS_DOMAINS` logs an error and carries on
  over plain HTTP. It does not refuse to start. Neither does a TLS listener that
  cannot bind its port — see *If this process terminates TLS*.
- Changing your own password ends every other session you have, and only those.
- API tokens carry no rights of their own. Every request is evaluated against the
  owner's current role, so changing or revoking that role bites on the next call.
- Whoever holds `roles:write` can grant themselves `settings:manage`, and
  `settings:manage` is the installation. That is the cost of Settings following
  granted rights rather than the built-in account, and it is intentional — but it
  means `roles:write` is an installation-level right, not a personnel one.

## Troubleshooting

| Symptom | Cause | What to do |
| --- | --- | --- |
| `cannot reach the configured database` and the process exits | the connection was proven and refused | the message names the dialect, the name and the host. Remove `DB_DIALECT` and `configs/datasource.json` to choose interactively instead |
| The installer appears on an installation that was working | nothing is configured any more — a lost volume, or a working directory that changed | check where `configs/datasource.json` is expected to be, and do not answer the installer until you know |
| The container is healthy but nobody can sign in | the healthcheck is satisfied by the installer | ask `/api/v1/branding` for a `version` field |
| A setting was changed and nothing happened | it needs a restart | *Settings* lists what is pending — but not a same-dialect database change or the trace sample ratio. Restart anyway if you changed either |
| TLS was enabled and the site is still plain HTTP | the listener could not bind, and that does not stop the process | check the log for `serving HTTPS on :443`; on a host, grant `CAP_NET_BIND_SERVICE` |
| `docker compose … -f compose.tls.yaml` refuses to start | `TLS_DOMAINS` or `TLS_EMAIL` is unset | both use the error form and are required |
| A setting was cleared back to "follow the configuration file" and still applies | the in-application restart inherited the exported variable | stop and start the process properly |
| Saving the database connection appears to do nothing | it applies at the next start, on purpose | restart |
| Every page load feels slow after an upgrade | assets are revalidated, not re-sent — check that your proxy is not stripping `ETag` or `If-None-Match` | |
| A directory run refuses with a ratio message | more accounts would be deleted than the guard allows | check the base DN and the filter first. That message is almost always right |
| The version in `/.well-known/health` disagrees with the footer | you are on a build from before this was fixed | the footer was always right |
