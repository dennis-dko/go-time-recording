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

Two overlays add a service to A or B rather than being a choice of their own,
and combine with each other in any order:

| Overlay | Adds | Include it when |
| --- | --- | --- |
| [`compose.tracing.yaml`](compose.tracing.yaml) | a Jaeger to receive traces | you are about to look at where the time goes |
| [`compose.ldap.yaml`](compose.ldap.yaml) | an OpenLDAP to sign in against | you have no directory and want one — read its header first, most installations should not |

Neither sets anything on the application. Both are switched on under *Settings*
in the running instance, because that is where the setting that wins is stored.

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

**On Windows, the first run is refused by SmartScreen** — *unknown publisher*,
with *Run anyway* behind *More info*. Every unsigned executable gets that, and no
build setting removes it; only a code signing certificate does. Check the
download against `SHA256SUMS`, then either press through the dialog or clear the
download mark first:

```powershell
Get-FileHash .\go-time-recording_v1.2.3_windows_amd64.exe -Algorithm SHA256
Unblock-File .\go-time-recording_v1.2.3_windows_amd64.exe
```

Rolling this out to more than a handful of machines is the point at which a
certificate is worth its yearly cost — the README weighs the kinds under
*Windows says "unknown publisher"*. A Windows service installed with `sc create`
or NSSM does not show the dialog at all, because nobody double-clicks it.

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

**And that failure does not stop the process.** The service comes up,
`systemctl status` says `active (running)`, and the installation serves
**unencrypted** HTTP on `HTTP_PORT`. It is now loud about it — the bind happens
before startup returns, so the log carries
`could not start HTTPS: cannot bind :443 …; continuing without HTTPS` rather
than a line from inside a goroutine — but the unit still starts. After enabling
TLS, check the log for `serving HTTPS on :443` and actually connect to the name
over HTTPS. Do not take "the unit started" as evidence.

**The plain listener stays bound on all interfaces, and answers only this
machine.** GoFr binds every interface and offers no way to say otherwise, so the
socket is there either way. While HTTPS is being served from this process, that
port serves the loopback address the TLS front end dials from and answers
everything else with a 308 to the encrypted address — so the headers that come
with an open port, `X-Forwarded-For` and `X-Forwarded-Proto`, can no longer be
written by whoever found it. When HTTPS did **not** come up, it serves the
network as before: redirecting to a port with nothing behind it would turn a bad
configuration into an outage.

Firewalling `HTTP_PORT`, or exposing only 80 and 443 on the public interface, is
still the tidier arrangement and nothing here argues against it.

## D · Bare container

```bash
docker run -d -p 8000:8000 -v gtr-data:/data \
  ghcr.io/dennis-dko/go-time-recording:v1.2.3
```

With no `DB_DIALECT` this serves its **installer** and waits — that is on
purpose. Setting `DB_DIALECT` skips it, which is what Compose does.

The image bakes in exactly one variable:

```
DB_NAME=/data/go-time-recording
```

That is the path already filled in if you pick SQLite in the installer, which is
what puts the database on the volume instead of inside the container. It
configures nothing by itself, because `DB_DIALECT` is deliberately absent.

Everything else the image runs on comes from `configs/.env` inside it, at layer
1 — the ports included. It used to set `HTTP_PORT` and `METRICS_PORT` here as
well, which was the same value written twice and had one consequence worth
knowing if you are on an older image: being real environment variables they beat
a `configs/` directory mounted over the image's own, so an operator who set a
port in their own file was overridden by the image with nothing on screen to say
so. Mounting your own `configs/` now actually decides the ports.

`docker run -e HTTP_PORT=…` still wins over both, because a real environment
variable still beats a file.

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
administrator who has already changed it.

So the shipped `configs/.env` sets only what no screen can administer:

| Still in the file | Why it cannot be a screen |
| --- | --- |
| `HTTP_PORT`, `METRICS_PORT` | the listeners are bound while starting; Settings can switch the metrics endpoint off but never move its port |
| `TLS_*`, `HSTS_MAX_AGE` | same, and a wrong value makes the instance unreachable rather than merely wrong |
| `DB_DIALECT`, `DB_NAME` | this is what decides whether there is a database to store a setting in |
| `UI_ENABLED`, `AUTH_ENABLED` | either one switched off removes the screen that would switch it back |
| `SHUTDOWN_GRACE_PERIOD` | read by the framework at start |
| `APP_NAME` | see below — it is not the instance title |

Six values used to sit there **as well as** in Settings — the log level, the
session lifetime, the daily cap, the two rate-limit figures and the directory
deletion ratio. They are gone from the file. Nothing changed behaviourally,
because every built-in fallback was already identical to the line that was
removed; what changed is that there is now one place per value instead of two,
and the file's copy was always the losing one.

**`APP_NAME` is not the instance title.** The title under *Settings → Appearance*
renames the header, and the browser tab where nothing separate is written for it
(*Browser tab* beside it, for an installation whose name is too long to be a tab).
`APP_NAME` is the issuer an authenticator
app shows beside an enrolled two-factor account — read once at start, administered
by no screen — and it also seeds the initial title, so naming the instance in the
environment saves naming it again. Changing it invalidates no enrolled codes, but
phones already enrolled keep showing the old name.

### The two example files here

They are not interchangeable.

| File | Read by | For |
| --- | --- | --- |
| [`.env.example`](.env.example) | **docker compose** | deployments A and B |
| [`.env.binary.example`](.env.binary.example) | **the process** | deployment C |

`.env.example` carries only what the compose files interpolate — database
credentials, the image tag, the TLS domain, and the two directory passwords if
that overlay is included. Several of its values mean nothing beside a binary.
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

## Serving HTTPS everywhere

Two routes, and which one applies is decided by whether the name this
installation answers to is one a public authority can be asked about.

**A public name.** `TLS_ENABLED=true` with `TLS_DOMAINS`, and Let's Encrypt
issues and renews the certificate by itself. Port 80 has to stay reachable from
outside, because that is where the challenge is answered.

**Anything else** — an office network, a hostname that resolves nowhere outside
it, an address with no public name at all. Let's Encrypt cannot go there, and
that is most installations of this application. Point it at a certificate you
already have:

```bash
TLS_ENABLED=true
TLS_CERT_FILE=/etc/gtr/fullchain.pem
TLS_KEY_FILE=/etc/gtr/privkey.pem
```

Nothing is requested from anybody: no domain, no challenge, no authority, so
port 80 is only the redirect. The certificate can come from a company authority —
which the machines already trust, so browsers say nothing — or be made for the
purpose, which they will warn about once per machine until it is trusted:

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 825   -keyout privkey.pem -out fullchain.pem   -subj "/CN=zeiterfassung.intern"   -addext "subjectAltName=DNS:zeiterfassung.intern"
```

A certificate that cannot be read stops the start rather than being discovered by
the first visitor, and `TLS_ENABLED` with neither route configured refuses to
pretend: it says so and carries on over plain HTTP rather than claiming HTTPS it
cannot serve.

## Behind a reverse proxy that is already there

Apache, nginx, or whatever already answers for the name. Then this process does
not serve HTTPS at all - `TLS_ENABLED=false`, which is the default - and the
proxy passes to its plain port.

### It needs a name of its own, not a path

`https://firma.example.com/zeiterfassung/` does not work, and cannot be made to
work from the proxy side. The interface asks for `/app.js`, `/app.css`,
`/theme.js` and `/favicon.ico`, and the script's API base is `/api/v1` - all
absolute, and there is no setting that moves them. Under a sub-path the browser
would look for those at the root of the site and find whatever else is there.

So: its own hostname, or its own virtual host. Everything below assumes that.

### Which deployment

**Compose is the one built for this.** `deploy/compose.yaml` publishes the
application to the loopback interface and nowhere else:

```yaml
ports:
  - "127.0.0.1:8000:8000"
```

A proxy on the same host reaches it; the network does not. Nothing further to
close. Do *not* add `compose.tls.yaml` - that overlay is for letting this
process terminate TLS itself, which is the other arrangement.

**The single binary works too, and leaves the port open.** GoFr binds every
interface and offers no way to say otherwise, and the guard that refuses network
traffic on the plain port only runs while this process is serving HTTPS itself -
which behind a proxy it is not. So that has to be closed by hand:

```bash
sudo ufw deny 8000/tcp
```

Anything that reaches the port can also claim to be a proxy: set its own
`X-Forwarded-For`, and have no rate limit on guessing a password.

### The whole configuration

`/etc/apache2/sites-available/zeiterfassung.conf`:

```apache
# Port 80 exists for the redirect and for certbot's challenge.
<VirtualHost *:80>
    ServerName zeiterfassung.firma.example.com

    RewriteEngine On
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^ https://%{SERVER_NAME}%{REQUEST_URI} [R=308,L]
</VirtualHost>

<VirtualHost *:443>
    ServerName zeiterfassung.firma.example.com

    SSLEngine on
    SSLCertificateFile    /etc/letsencrypt/live/zeiterfassung.firma.example.com/fullchain.pem
    SSLCertificateKeyFile /etc/letsencrypt/live/zeiterfassung.firma.example.com/privkey.pem

    # Whatever a caller sent for itself goes, before Apache sets its own. The
    # sign-in rate limit counts per address and reads these; left alone, the
    # caller chooses which address it is counted as.
    RequestHeader unset X-Forwarded-For   early
    RequestHeader unset X-Forwarded-Host  early
    RequestHeader unset X-Forwarded-Proto early

    # The one Apache does not add by itself. Without it this process cannot tell
    # the browser is on HTTPS: the session cookie is written without Secure and
    # no HSTS is sent.
    RequestHeader set X-Forwarded-Proto "https"

    ProxyPreserveHost On
    ProxyRequests Off

    ProxyPass        / http://127.0.0.1:8000/
    ProxyPassReverse / http://127.0.0.1:8000/

    # The announcement stream - a restart coming, an update installed - is a
    # response that deliberately never ends. Compressed, it is buffered rather
    # than delivered.
    SetEnvIfNoCase Request_URI "^/api/v1/events" no-gzip dont-vary

    ErrorLog  ${APACHE_LOG_DIR}/zeiterfassung-error.log
    CustomLog ${APACHE_LOG_DIR}/zeiterfassung-access.log combined
</VirtualHost>
```

```bash
sudo a2enmod proxy proxy_http headers ssl rewrite
sudo a2ensite zeiterfassung
sudo apache2ctl configtest
sudo systemctl reload apache2
```

### Do not add security headers here

This process sends its own: a Content-Security-Policy, HSTS, `X-Frame-Options`,
`Referrer-Policy` and `Permissions-Policy`. A second CSP from the proxy does not
replace the first - a browser enforces every policy it is given, so the effect is
the strictest of both, and the interface stops working for reasons that appear in
the browser's console and nowhere else.

HSTS arrives on its own once `X-Forwarded-Proto: https` does.

### What this arrangement does not need

`ProxyTimeout` does not have to be raised for the announcement stream. It writes
a comment down an idle connection every twenty seconds, which is under every
proxy default worth naming - the browser reconnects if one is closed anyway, so
the cost of getting this wrong is reconnections rather than a broken screen.

Nothing has to be configured for WebSockets: there are none. The stream is
server-sent events over an ordinary HTTP response.

## The database has to exist first

For PostgreSQL and MySQL, **yes — create the database and the account before
pointing the application at them.** It creates its own *tables* on first start
and every migration after that; it does not create the database itself, and it
does not ask for credentials that could.

That is deliberate rather than an omission. A connection allowed to create
databases is a connection allowed to drop them, and this application would be
holding it for the rest of its life to use it once.

```sql
-- PostgreSQL
CREATE DATABASE "go-time-recording";
CREATE USER gtr WITH PASSWORD 'choose-something';
GRANT ALL PRIVILEGES ON DATABASE "go-time-recording" TO gtr;
-- PostgreSQL 15 and later also need the schema itself:
\connect "go-time-recording"
GRANT ALL ON SCHEMA public TO gtr;
```

```sql
-- MySQL / MariaDB
CREATE DATABASE `go-time-recording` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'gtr'@'%' IDENTIFIED BY 'choose-something';
GRANT ALL PRIVILEGES ON `go-time-recording`.* TO 'gtr'@'%';
```

`utf8mb4` matters on MySQL: names, project titles and the notice on the sign-in
screen are whatever people type, and the older `utf8` is three bytes and cannot
store all of it.

The account needs `CREATE TABLE`, `ALTER`, `INDEX` and the ordinary read and
write rights *inside* that database — the grants above give it those. It never
needs to create a database.

**SQLite creates its own file**, so there is nothing to prepare. That is what
`task dev` and the single binary use unless told otherwise.

The installer tests the connection before writing it down, so a database that
does not exist yet is reported on that screen rather than discovered at the next
start. The compose files here create the database themselves, through the
official image's `POSTGRES_DB` and `MYSQL_DATABASE` — which is the same act,
done by the container instead of by you.

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
| the database connection | never swapped under live requests | yes — dialect, host, port, name, user and SSL mode |
| the database password | same | yes, as the name of the setting alone |
| log level | the logger's level is read at start | yes |
| metrics off | the port is bound at start | yes |
| trace exporter, collector URL, sample ratio | the exporter is built at start | yes |
| the directory sync schedule | a cron job is registered at start | yes |

Applying immediately: the operational limits, the whole directory connection, the
instance timezone, branding and the logo, maintenance mode, users and roles.

The connection is compared whole. It used to be compared by dialect alone, on
the grounds that a changed host is a change to the same connection — which
describes what the card *says* and answers the wrong question, because the
connection is opened once while the application starts. Moving the database to
another host is exactly as pending as moving it to another dialect, and it now
reads as one line: `postgres db:5432/gtr as app` → `postgres db2:5432/gtr as
app`. A default port and an omitted one are the same connection here as they
are in fact, so spelling out `5432` is not reported as a change.

A changed password appears as *Database password* with nothing beside it. The
old one is not printed next to the new one on an administration screen, and the
card renders an entry with no before and after as the name of the setting alone
rather than as "none → none".

Saving the database form says which of the two it was. It reports *Settings
saved* when the comparison finds nothing and *Applied on the next start* when it
does — it used to promise a restart on every press, including on a form somebody
had only opened to look at.

**The in-application restart button replaces the process image, and passes the
current environment on.** That matters in one case: a setting you cleared back to
*follow the configuration file* is **not** restored by it, because the variable
the previous process exported is inherited and still beats the file. The same
goes for deleting `configs/datasource.json` — the inherited `DB_DIALECT` keeps
the old connection instead of bringing the installer back. Those need a real stop
and start.

On Windows the button is not offered at all: there is no `execve`, so the nearest
equivalent would leave a window with no application running. The card says so.

## If the tab icon does not appear

The configured logo is served at `/favicon`, from an address carrying a
fingerprint of its contents, and written into the page by the server. Firefox,
Chrome and Edge all take it: the Firefox suite checks that by reading what
Firefox recorded in its own icon store, for an SVG that states its size, one with
only a viewBox, one with neither, and a raster wordmark 2000px across.

So if a tab is empty, it is one of these three, in this order:

**The build is older than the fix.** The icon used to be patched in by the page
after loading, which Chrome honours and Firefox ignores - the exact shape of
"works in Chrome, empty in Firefox". Check the footer: it names the version.

**Firefox is showing what it remembered.** It keeps an icon against a page in
`favicons.sqlite` and reuses it, including the memory of a page that had none.
`Ctrl`+`Shift`+`R` reloads past it; *Forget About This Site* clears it outright.

**The logo cannot be seen at that size.** A tab gives it sixteen pixels. A wide
wordmark in near-black is a smudge on a dark tab strip - present, adopted,
invisible. The appearance screen shows the logo at exactly that size beside the
other two previews, which is the quickest way to tell this apart from the two
above.

To take the browser out of it entirely, open `/favicon` directly. What comes back
is what any tab is given.

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

### Tracing

[`compose.tracing.yaml`](compose.tracing.yaml) is a third overlay that runs a
Jaeger beside the application. It combines with the other two in any order,
because it only adds a service:

```bash
docker compose -f compose.yaml -f compose.tracing.yaml up -d
docker compose -f compose.yaml -f compose.tls.yaml -f compose.tracing.yaml up -d
```

**The overlay supplies the collector and nothing else.** It sets no tracing
variables on the application on purpose, because they would not be the setting
that wins: tracing is administered in the running application, and what is stored
there is applied over the environment at the next start. So switch it on under
*Settings → Logging, metrics and tracing*:

| Field | Value |
| --- | --- |
| Trace exporter | `OTLP` |
| Collector as host:port | `jaeger:4317` — **no** `http://` in front |
| Share of traces recorded | `1` while investigating, lower if left on |

The scheme matters: that string goes to a gRPC dialer, which reads `http://` as
part of the host name and then resolves nothing.

**Then restart the application.** The exporter is built while it starts, so a
saved setting does nothing until it does. All three go into it, so all three
appear in the pending list and the Settings screen offers the button.

The share used to be missing from that list — it needed the restart and nothing
said so — and this manual described the gap as though it were the design. It is
compared now.

**Reading the traces.** The browser is on `127.0.0.1:16686` and asks nobody to
sign in, so it is not published to the network — traces carry request paths and
the identifiers in them. Reach it through a tunnel:

```bash
ssh -L 16686:127.0.0.1:16686 <server>   # then http://127.0.0.1:16686
```

**Traces are held in memory and bounded** (`MEMORY_MAX_TRACES=20000`). A Jaeger
restart, a host reboot or an image upgrade takes every trace with it, including
the ones you were in the middle of reading. That is the right trade for a
debugging aid that is switched on for an afternoon; an installation that needs
traces to survive wants a badger store on a volume with a span TTL, and has to
plan that disk the way it plans the database's.

Two deliberate omissions: the OTLP port is not published, so nothing outside the
compose network can write spans; and Jaeger is **not** a startup dependency of
the application, so a broken collector can never keep the time recording from
starting. The price of the second is that spans exported before Jaeger is ready
are dropped.

To turn it off again, drop the overlay from the command **and** set the exporter
back under *Settings*. The stored setting outlives the container, so leaving it
behind means an application starting up and failing to export, every time.

### A directory, if this installation has none

[`compose.ldap.yaml`](compose.ldap.yaml) is a fourth overlay, and the one to
think about before including. It runs an OpenLDAP beside the application:

```bash
docker compose -f compose.yaml -f compose.ldap.yaml up -d
```

**Most installations should leave it out.** The application is a directory
client, so an installation that already runs an Active Directory, an OpenLDAP or
a FreeIPA points at that one under *Settings* and needs no service here. An
installation whose people sign in with a password, a passkey or an authenticator
code needs no directory at all — adding one is a second stateful service to back
up and a second password store to keep, for the same people, and a directory
account cannot hold a passkey because its password lives in the directory. Two
cases are worth it: seeing what the feature does before pointing it at the real
directory, and genuinely having none and wanting one.

Like the collector, it sets nothing on the application: the connection is
administered under *Settings* and what is stored there is what wins. The header
of the file lists every field, and one of them is worth repeating here — the id
attribute must be `entryUUID`, not `uid` and not the mail address. slapd assigns
it and nobody can change it, so somebody who marries and changes their address
stays the same account. Keyed on the address, that person is a departure and an
arrival, and the next synchronisation deletes what the first one recorded.

Three things differ from the other overlays:

- **It needs `deploy/ldap/` beside it.** The image is built rather than pulled,
  because the OpenLDAP images most compose files reach for are withdrawn from
  Docker Hub or archived upstream. Copying `compose.yaml` and `.env` alone is
  enough for every other arrangement and is not enough for this one. It is also
  the image the test suite runs against, so every bind and search this
  repository exercises is exercised against exactly what a deployment starts.
- **Two passwords in `.env`**, both required and neither defaulted:
  `LDAP_ADMIN_PASSWORD` writes the directory and the application never sees it;
  `LDAP_BIND_PASSWORD` is the read-only account typed into the Settings card.
  They are hashed before they reach any file inside the container.
- **It comes up empty.** A suffix, an `ou=people`, and the bind account —
  no people. Invented ones would arrive in the time recording on the first
  synchronisation, named after nobody and awkward to remove once entries hang
  off them. The header has the `ldapadd` for a real one; a `mail` attribute is
  required, because that is what an account is keyed on.

Back it up separately. It is a stateful service on the `ldap-data` volume and is
not in the database dump below:

```bash
docker compose exec -T openldap slapcat -f /etc/ldap/slapd.conf | gzip > ldap-$(date +%F).ldif.gz
```

## Updating from the interface

*Settings* carries a **Version** card. It says what is running, what the newest
release is, and what can be done about it here - which differs by deployment, and
that difference is the whole of it.

| | What the card offers |
| --- | --- |
| **C** Single binary | A button. It downloads the release's binary for this platform, checks it against the `SHA256SUMS` published beside it, and puts it where the running file is. |
| **A/B/D** Container | No button, and the command to run instead. |

**Why no button in a container.** A binary swapped inside a container is undone by
the next `docker compose up` - which is the moment somebody is most certain the
update took. Offering it there would be offering an update that silently reverts,
so the card says `docker compose pull && docker compose up -d` instead. That is
not a gap in the feature; it is what updating a container is.

**Nothing is written into place unverified.** The download is hashed while it is
written and compared against the release's own `SHA256SUMS`, read from the same
release. This is code that will be executed as the application on the next start.

**It does not restart by itself.** Replacing the file and replacing the process
are separate acts with separate failure modes, and on Windows the second one does
not exist - so the card says which case it is. On Linux the restart button below
it applies the new version; on Windows the application has to be started again by
hand, and the card says so. Until then the card reports the downloaded version as
waiting rather than offering the same update twice.

### What everybody else sees

Everyone signed in is told, at the moment the update starts rather than after it
finished. Each browser holds one connection open to `/api/v1/events`, and the
update writes down it - so the notice arrives in the same second, on an idle
screen, without anybody polling for it.

There are three things it can say, and they mean different things to somebody in
the middle of typing:

| | What is said | Can they keep working? |
| --- | --- | --- |
| **Installing** | A new version is being downloaded and checked. | **Yes, entirely.** This takes tens of seconds and changes nothing about the running application. |
| **Restarting** | The new version is in place and this process is about to be replaced. | **For a few seconds, no.** Requests in flight fail; the page reloads itself when the application answers again. |
| **Pending** | Installed, and waiting for somebody to restart the application by hand. Windows. | **Yes.** Nothing changes until that restart happens. |

An update that fails its checks says so too, and the banner goes: a promise of a
restart that is not coming is worse than no notice at all.

**Nothing is lost that had been saved.** The restart is a process being replaced,
not a database being touched - every entry already submitted is on disk. What is
lost is a form somebody had filled in and not yet submitted, which is why the
warning comes before the download rather than with the restart: the download is
the notice period.

Requests that fail during those seconds do not raise errors on screen. The banner
already says what is happening, and a dozen red toasts on top of it would only
obscure it.

**Nothing is installed that has not been run once.** The checksum proves the
bytes are the ones the release published; it says nothing about whether they run
*here* - a build for the wrong libc, an architecture that looked right, a release
that is simply broken. So the downloaded file is executed with `--version` before
the swap, and discarded if it will not answer. This is the cheapest possible
question and it catches the failure that would otherwise be discovered by the
application not coming back.

### The file keeps its name

A release is published as `go-time-recording_v1.2.3_windows_amd64.exe`, so a
manual download says which version it is in its own name. An update installed
from the interface does not rename anything: the new version is written to the
path the running one already occupies, whatever that path is called.

That is deliberate and not negotiable. The path is the installation - a systemd
unit, a Windows service, a shortcut, a scheduled task and whatever scripts exist
all name it. An update that renamed the file would be an update that takes the
application offline and leaves no obvious reason why.

Which version a file is stays answerable three ways:

```bash
./go-time-recording --version
```

On Windows, right-click → *Properties* → *Details*: the published binaries carry
a version resource, so Explorer shows the new version after an update without
being asked. And the footer of the interface names it beside the platform.

### Going back

The version being replaced is kept, on both platforms, beside the binary as
`go-time-recording.old`. It stays there until the *next* update, which removes it
as its first act - so there is always exactly one version to go back to, and
never two.

That matters because starting is not serving. A new version can run, pass the
check above, replace the old one, and still fail on a migration, a port already
taken, or a certificate it cannot read. At that point there is no interface to
press anything in, which is why the way back is a flag rather than a button:

```bash
./go-time-recording --rollback
# the previous version is back in place; start it again
```

It moves two files: the version that would not serve becomes
`go-time-recording.failed`, kept so it can be looked at, and `.old` takes its
name back. No database, no configuration, nothing else touched. Start it as you
normally would.

Under systemd, `systemctl stop`, roll back, `systemctl start` - the unit file
still points at the same path.

On Windows the running binary is renamed aside rather than deleted, because
Windows will not delete a file it is executing, so the same arrangement falls out
of the platform rather than being arranged.

### Switching it off

```bash
# An installation that must not reach the internet at all. On by default: an
# installation that never learns a fix exists is not safer for it.
UPDATE_CHECK=false

# Or point it somewhere reachable - a mirror, a proxy, a fork's own releases -
# rather than switching the whole thing off.
UPDATE_FEED=https://api.github.com/repos/you/your-fork/releases/latest

# Only if the check starts answering 403. See below for why it would.
UPDATE_TOKEN=
```

### When the check says 403

The feed allows a number of checks per hour **per address**, not per
installation. A dozen instances behind one office connection draw from the same
allowance, and so does everything else on that connection that talks to the same
service. Running out answers `403`, which reads as a permission problem and is
not one — nothing about this installation's rights has changed.

The card says so in those words, with what the feed itself replied folded away
underneath. Three ways out, in order of how much trouble they are:

1. **Wait.** The allowance refills each hour, and the message says when.
2. **Set `UPDATE_TOKEN`.** Any token the feed accepts; the count is then per
   token and far larger. It needs no permissions — this only reads a public
   release list.
3. **Point `UPDATE_FEED` at a mirror** you control, which is the answer for an
   installation that should not be asking a public service on a schedule anyway.

Nothing is broken while this lasts. The application runs on the version it has;
only the question "is there a newer one" goes unanswered.

Only an account that administers the installation and has no working day of its
own sees the card at all - the built-in administrator, or somebody holding the
admin role. Not the combined role: this replaces the bytes that will be executed,
which is a different kind of decision from changing a setting.

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

There is no directory service in `compose.yaml`, because this application is a
directory client: you point it at the one you already run.
`compose.ldap.yaml` runs an OpenLDAP beside it for the two installations where
that is not true — trying the feature out, and having no directory at all.
Read its header before including it; most deployments should not.

### Who may run one

Editing the directory card, testing the connection and saving it need
`settings:manage`, like every other setting. Running a synchronisation does
not: **the preview, the button and the schedule field belong to the built-in
administrator alone**, and the card is not shown to an account that would be
refused it.

The distinction is what a run does rather than what it reads. Configuring a
directory is configuration; deleting every account the directory no longer holds
along with everything those people recorded is not. The schedule sits on the
same side of that line — it is the same deletion performed later and unattended
— and it used to sit on the other one, so the caution the buttons were given
could be walked around by typing five numbers into the field between them.

An installation whose administration has been handed to a `user-admin` account
therefore keeps one thing behind the built-in login. That is deliberate: it is
the account that exists before anybody has chosen anything, and the irreversible
operation is the right thing to keep there.

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
| A setting was changed and nothing happened | it needs a restart | *Settings* lists what is pending. Two things used to be missing from that list — a same-dialect database change and the trace sample ratio — and both are compared now |
| TLS was enabled and the site is still plain HTTP | the listener could not bind, and that does not stop the process | check the log for `serving HTTPS on :443`; on a host, grant `CAP_NET_BIND_SERVICE` |
| `docker compose … -f compose.tls.yaml` refuses to start | `TLS_DOMAINS` or `TLS_EMAIL` is unset | both use the error form and are required |
| A setting was cleared back to "follow the configuration file" and still applies | the in-application restart inherited the exported variable | stop and start the process properly |
| Saving the database connection appears to do nothing | it applies at the next start, on purpose | restart |
| Every page load feels slow after an upgrade | assets are revalidated, not re-sent — check that your proxy is not stripping `ETag` or `If-None-Match` | |
| A directory run refuses with a ratio message | more accounts would be deleted than the guard allows | check the base DN and the filter first. That message is almost always right |
| The version in `/.well-known/health` disagrees with the footer | you are on a build from before this was fixed | the footer was always right |
