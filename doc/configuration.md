# Configuration

GoFr reads `configs/.env` first, then overlays `configs/.$APP_ENV.env` on top
of it. So `.env` holds every default, and the environment files beside it hold
only what actually differs — `.dev.env` is one line, `.prod.env` is none.

Four layers, lowest to highest:

| | Source | Set by |
| --- | --- | --- |
| 1 | `configs/.env` | the repository — every default |
| 2 | `configs/.$APP_ENV.env` | the repository — differences per environment |
| 3 | real environment variables | the deployment: compose, systemd, the shell |
| 4 | `configs/datasource.json` | the **installer**, and *Settings → Database* |

Layer 3 is why a deployment needs no file edits: `deploy/compose.yaml` sets
everything it needs as environment variables. Layer 4 is deliberately on top of
it — changing the database in the interface is an explicit act, and it would be
surprising for it to be silently ignored because a stale variable was still set
somewhere. It is also narrower than the others: it only ever supplies `DB_*`, and
only the fields it actually holds, so an environment `DB_HOST` survives a
`datasource.json` that omits one.

With `APP_ENV` unset, layer 2 falls back to `configs/.local.env`. That file is
not in the repository and is gitignored: it is the right place for a personal
database or a debug log level, and the wrong thing to commit, because it would
silently apply to everyone.

`APP_ENV` itself has to come from layer 3. GoFr reads it before it opens any of
these files, so writing `APP_ENV=prod` into `configs/.env` selects no overlay at
all — and does it quietly, which is the part worth knowing.

## What belongs in a file, and what belongs in the application

**Bootstrap** settings can only be set in layers 1–3. They decide how the
process starts, so an application that has not started cannot administer them —
and getting one wrong must not be fixable only from a screen it takes away:
ports, `TLS_*`, `AUTH_ENABLED`, `UI_ENABLED`, and `LDAP_SYNC_SCHEDULE`, which is
the only cron expression there is — the session prune runs at 03:00 and is not
configurable at all.

**Administered only** - `SESSION_LIFETIME`, `MAX_DAILY_HOURS`, `RATE_LIMIT`,
`RATE_LIMIT_WINDOW` and `LDAP_SYNC_MAX_DELETE_RATIO` - no longer appear in
`configs/.env` at all. They used to, beside being administrable, and every
built-in fallback was already identical to the line in the file: two places
holding one value, of which the file's was always the losing copy. Setting one
in the environment still decides what a fresh installation begins with, before
anybody opens that screen; the templates in `deploy/` name them commented out
for exactly that case.

`APP_NAME` stays, and it is worth knowing why, because it looks like the
instance title and is not. The title under *Settings* renames the header, and the
browser tab where nothing separate is written for it; `APP_NAME` is the issuer an authenticator app shows beside an
enrolled two-factor account, which no screen administers - and it seeds the
initial title, so naming the instance in the environment saves naming it twice.

**At the next start** are administered too, but stored rather than applied,
because GoFr reads them while it starts up: the `DB_*` connection,
`LDAP_SYNC_SCHEDULE`, and `TRACE_EXPORTER`, `TRACER_URL` and `TRACER_RATIO`. What
is stored wins from the next start onwards, and a banner across the top of every
screen lists what is still waiting, for whoever may do something about it.

`LOG_LEVEL` is administered too, is likewise out of `configs/.env` now, and is
the one of these that does not wait: the level is applied to the lines on their
way out of the process rather than by GoFr's logger, so a saved level holds from
the next line. The one file that still names it is `configs/.dev.env`, which is
what "follow the configuration file" means for a development run that has no
stored setting yet.

The **timezone and the LDAP connection appear in no file at all**. Both are
administered entirely in the application — a second place to write them would
only disagree with the first.

The default column is the value built into the binary, which applies when
nothing sets the variable.

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_NAME` | `Time Recording` | the two-factor issuer, and the initial title until one is set under Settings |
| `HTTP_PORT` | `8000` | API and web interface |
| `METRICS_PORT` | `2121` | Prometheus endpoint; `0` switches it off |
| `DB_DIALECT` | – | `sqlite`, `postgres` or `mysql`. **Empty serves the installer** |
| `DB_NAME` | – | with SQLite, the file name without `.db` |
| `SETUP_TOKEN` | generated | what the installer asks for; logged when generated |
| `UI_ENABLED` | `true` | `false` runs the binary as a headless API |
| `AUTH_ENABLED` | `true` | `false` gives **every** caller full admin rights |
| `SECRET_KEY` | empty | base64 of 32 bytes. Encrypts the second factors and the directory password in the database. Empty stores them in the clear, as before, and logs a line saying so. **Losing it costs every enrolled second factor** |
| `SESSION_LIFETIME` | `12h` | how long a sign-in stays valid. **Administered under Settings**; not in `configs/.env` |
| `TLS_ENABLED` | `false` | HTTPS with Let's Encrypt |
| `TLS_DOMAINS` | – | comma separated; the names Let's Encrypt is asked about. One of this or the two below is required when TLS is on |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | – | a certificate this installation already has, in PEM. With both set nothing is requested from any authority — which is the only way to serve HTTPS on a name Let's Encrypt cannot reach |
| `TLS_EMAIL` | – | receives expiry warnings |
| `TLS_PORT` / `TLS_REDIRECT_PORT` | `443` / `80` | HTTPS, and the ACME/redirect listener |
| `TLS_STAGING` | `false` | Let's Encrypt test authority |
| `HSTS_MAX_AGE` | `8760h` | only sent over HTTPS. `0` switches it off, and tells browsers that stored it before to forget it |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW` | `30` / `1m` | sign-in and token requests per client. **Administered under Settings**; not in `configs/.env` |
| `TRUSTED_PROXIES` | empty | comma separated CIDR ranges or addresses whose `X-Forwarded-For` the rate limiter may believe. Loopback is always believed, so the built-in HTTPS front end needs no entry — this is for a proxy that is somewhere else, such as an nginx in the next container. Leave it empty when nothing is in front: a forwarded header from the open network is written by the client, so believing it would hand out a fresh sign-in budget per request |
| `UPDATE_CHECK` | `true` | ask the release feed whether a newer version exists. `false` for an installation that must not reach the internet. The answer is kept for six hours, so the *Version* card can be up to that far behind a release you have just cut — **Check for updates** on the card asks at once, at most once every five minutes |
| `UPDATE_FEED` | GitHub | where to ask — a mirror, a proxy, or a fork's own releases |
| `UPDATE_TOKEN` | empty | identifies this installation to the feed. Almost never needed: checking takes no credentials. The limit is counted **per address**, so a dozen instances behind one office connection share sixty checks an hour, and running out answers `403` |
| `LDAP_SYNC_SCHEDULE` | empty | cron for the directory reconciliation; empty means manual only. Administered under *Settings* as well, where what is saved wins from the next start |
| `LDAP_SYNC_MAX_DELETE_RATIO` | `0.5` | refuse a run removing more than this share of directory accounts. **Administered under Settings**; not in `configs/.env` |
| `MAX_DAILY_HOURS` | `24` | instance-wide cap per person per day, at most `24`; anything else is read as the default. **Administered under Settings**; not in `configs/.env` |
| `LOG_LEVEL` | `INFO` | `DEBUG`…`FATAL`; anything else is read as `INFO`. **Administered under Settings**; in `configs/.dev.env` only |
| `TRACE_EXPORTER` | empty | `otlp` or `jaeger`; empty exports nothing |
| `TRACER_URL` | – | the collector as `host:port`, **without** a scheme |
| `TRACER_RATIO` | `1` | share of traces recorded, `0`–`1` |

## What can be changed from the interface, and what cannot

Five of the values above are **operational limits** rather than deployment
facts, so *Settings → Operation and limits* overrides them while the
application runs — no restart, no file access. A field left empty keeps
following the file, and its value is shown as the placeholder, so it is always
visible what a blank field means. The screen also prints what is currently in
force, and *Reset* drops every override at once.

| Administered from the interface | Applies |
| --- | --- |
| `SESSION_LIFETIME` | to the next sign-in |
| `SESSION_IDLE` | to every session, from the next request |
| `MAX_DAILY_HOURS` | to the next booking |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW` | within seconds |
| `LDAP_SYNC_MAX_DELETE_RATIO` | at the next synchronisation |

Two of those bound the same thing from different ends, and the difference is
worth being clear about. `SESSION_LIFETIME` is absolute and starts at the
sign-in: how long one act of proving who you are is worth, whatever anybody does
with it. `SESSION_IDLE` is measured from the last request: whether anybody is
still there. A person working all morning keeps their session by the second rule
and eventually loses it by the first; the same person going home at noon loses it
by the second while the first would still have let them back in. What the page
asks by itself does not count - the check it makes once a minute, the event
stream reopening, the log screen following along - so a screen left open and in
view is signed out as surely as one that is closed.

The idle timeout is **off** until somebody sets it — signing people out of a
screen they left open is a decision about how an office works, not one to impose
on every installation on the day it updates. When a session does end, for either
reason, the interface goes back to the sign-in screen and says so, rather than
standing there refusing every click.

Values are bounded on save, because this is the one screen that can lock its
own administrator out: a session lifetime of a second would sign everyone out
mid-click, and a rate limit of one would refuse the very sign-in needed to
undo it.

The rest stays in the file **on purpose**, because getting it wrong would
remove the way back in:

| Stays in the file | Why |
| --- | --- |
| `AUTH_ENABLED` | switching it off from the interface would open the instance to anyone, and nobody could switch it back on |
| `UI_ENABLED` | it removes the interface that would restore it |
| `TLS_*` | the listener is bound at start-up; a wrong domain or port makes the instance unreachable |
| `HSTS_MAX_AGE` | a browser told to refuse plain HTTP keeps refusing for as long as the value said, whatever is served later |
| `DB_*` | it is the connection the settings themselves are read from |
| `*_SCHEDULE` | a cron job is registered once at start-up and cannot be re-registered live. `LDAP_SYNC_SCHEDULE` is the exception and only half of one: the field is on the directory card, so it can be set without file access, but it waits for a restart like everything else here — and it is the built-in administrator's, not `settings:manage`'s |
| `HTTP_PORT`, `METRICS_PORT` | bound at start-up — and GoFr refuses to start when something already holds the metrics port, so a port saved from a screen could stop the application together with the screen. *Settings* can only switch the endpoint **off**, which cannot fail |

`APP_NAME` is not administered here either — the instance title under
*Settings → Appearance* already overrides it, and two fields for one label
would only disagree.

For PostgreSQL also set `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD` and
`DB_SSL_MODE` — or configure them under **Settings**, where a *Test connection*
button probes them before you commit. A connection saved there is written to
`configs/datasource.json` and applied on the next restart; switching a live
database under running requests is not safe, so it is deliberately not done.
Saving does not try the connection, so that one can be prepared for a server
that is not up yet - but the restart button does: it refuses to restart into a
database that does not answer, because the start would end on it and nothing
would be left to put it right from. Any other restart still reads the file.

PostgreSQL needs a password here even where the server asks for none - any value
does then. The connection is written with the values as they are, and in that
form an empty password takes the database's name for itself, so the server opens
the database named after the account: measured, the tables went into `postgres`
while the log named `DB_NAME`. The installer and *Settings* refuse such a
connection, and one the driver would mangle in other ways, which
[the operations manual](../deploy/OPERATIONS.md) lists; an installation already
running on one keeps its data where it is and is told so at every start.

On an installation configured through the environment — a compose deployment, or
a container run with `DB_*` set — there is no such file, so the card is filled
with the running connection instead, as values, and says above them where it came
from. That note matters more than it looks: saving this form writes the file, and
the file is layer 4 above the environment, so pressing *Save* — changed or not —
takes the deployment's own settings out of use from the next start. Not pressing
it leaves the connection alone.

**The password is shown as a filled, masked box, and the page never holds it.**
The server sends only whether there is one; the dots stand for it, and the eye
beside them stays shut because there is nothing behind them to show. Saving or
testing the card with the box untouched keeps the password in force — the stored
one, or the environment's — and only for the same type, host, port and user: name
another server or user and the box empties, because that password is not sent
anywhere it was not given.

*Settings → Logging, metrics and tracing* works the same way, and for the same
kind of reason: GoFr binds the metrics port and builds the trace exporter inside
`gofr.New()`, so nothing administered afterwards could reach either of them.
What is saved there is stored in the database and read back out of it on the way
into the next start, before GoFr reads its own configuration — which is what
lets a stored value win over the file, including a stored *off*. A field left
following the configuration file keeps coming from there.

The log level is on the same card and does not wait. GoFr is left writing
everything, and the level is applied to each line on its way out of the process,
which is where the log viewer reads it too — so saving a level changes what the
very next line does. That was built when changing GoFr's own level under running
requests was a data race; GoFr has made that field atomic since (v1.60.0), and
the level is still applied on the way out.

The screen shows what the running process is actually doing beside what is
stored, because until the next restart those disagree, and it names the metrics
endpoint in full so it can be copied. A banner across the top of every screen
lists what is waiting — each setting with the value in force and the one that
will replace it — and offers to restart there and then, with the interface
waiting for the application to come back rather than leaving anyone to guess.

It is a banner rather than a card on that screen because what it reports is a
fact about the installation rather than about the screen it was saved on: an
administrator who saves something and goes back to work would otherwise have
nothing anywhere telling them the application is still running on the old
values. It cannot be dismissed for the same reason, and it is shown only to
accounts that may administer this installation.

What the banner reports is the difference between what this process is using and
what the next start would use — which is the stored value where there is one and
the configuration file's where there is not. Clearing a field is therefore a
change like any other, and appears as one.

The exception is a setting that would change nothing. The collector address and
the recorded share describe where spans go and how many of them, so with no
exporter at either end they describe nothing, and a restart in exchange for a
difference in nothing is worth nobody's outage. They are left out until
something exports.

The database is compared whole: the dialect, the host, the port, the name, the
user and the SSL mode, read as one line rather than as six. A changed password
is listed as *Database password* and nothing else — the old one is not printed
beside the new one on an administration screen, so that entry carries no before
and after and the card shows the name of the setting alone. Saving the form says
which of the two happened, and says *Settings saved* when nothing changed rather
than promising a restart to somebody who only opened it to look.

That restart does one of two things, and the banner says which before the button
is pressed.

Outside a container it replaces the process image rather than exiting and hoping
something starts it again. `execve` needs nothing outside the process, so there
is no arrangement in which pressing it leaves the installation down — whereas
exiting works under systemd with `Restart=` and turns the button into an off
switch for a binary started by hand. What it hands the new process is the
environment this one was **started** with, not the one it has by then, so the
restart reads what a stop and a start would read.

**In a container it exits**, and the container manager starts it again. That was
chosen while replacing the process still handed on the environment as it stood,
which in a container is most of the configuration: a setting cleared back to
*follow the configuration file* came back as the value the previous process had
exported — from a screen whose whole promise is that the next start uses what is
stored.

What starts it is the restart policy, which this process cannot see. The
deployment here sets `unless-stopped`, which restarts whatever the exit status,
and so does `always`. A container run without a policy stays down, and so does
one run under `on-failure`: the button ends the process without an error, which
is the one exit that policy does not restart. The sentence beside the button
names the policies.
Windows has no `execve`, so the button is not offered there and the banner puts
the reason where the button would have been. It appears when something is
actually waiting, which is the moment the limitation costs anything: a warning
that is on screen every time you look is furniture, read once and looked past
thereafter, including on the day it finally has something to say.

A restart from the button reads the configuration the way a stop and a start
would, in a container and outside one. Outside one it used not to: the process
handed on its own environment, into which the configuration file's keys and the
stored settings had been written by then, and a real environment variable beats
the file. So an edited `configs/.env` was not read, a setting cleared back to
*follow the configuration file* was not restored, deleting
`configs/datasource.json` did not bring the installer back, and an installation
that had stored no log level logged at `DEBUG` after its first restart. None of
the four needs a genuine stop and start any more.

Both of those were once things this banner could not tell you, and both are
compared now: a database change that keeps the dialect — another host, port, user
or password — and the recorded share of traces. An empty banner means nothing is
waiting, with the one deliberate exception named above.

On Windows, then, a saved setting takes effect when the application is next
started the way it was started. Two exporters are offered, `otlp` and `jaeger`.
Zipkin is not: GoFr still accepts it while warning that it is on its way out. Neither is GoFr's hosted exporter, which posts every span to a service
run by the framework's authors — not a thing to be able to switch on by picking
an entry from a list.

**The framework calls home by default, and nothing here switches that off.**
`GOFR_TELEMETRY` defaults to on — [`deploy/.env.binary.example`](../deploy/.env.binary.example)
writes that default out so it is at least visible — which means every start POSTs to
`https://gofr.dev/api/ping/up`, every shutdown to `.../ping/down`, and every
start also sends a document — application name and version, framework and Go
version, operating system, architecture — to
`https://gofr.dev/telemetry/v1/metrics`. None of it carries recorded time or
anybody's data, and all of it is outbound traffic an air-gapped or
egress-filtered deployment did not ask for. One line switches all three off:

```bash
GOFR_TELEMETRY=false
```

It is deliberately not administrable from *Settings*: it has to be settled before
the framework starts, which is the same reason the ports are not.
