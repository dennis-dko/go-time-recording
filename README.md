# Go-Time-Recording

Project time tracking as **a single self-contained binary**: the REST API and
the web interface live in the same executable (the UI assets are embedded with
`go:embed`). Starting it needs no asset directory and no migration step, and no
database server unless you choose one.

Built on [GoFr](https://gofr.dev), structured after [gogs](https://github.com/gogs/gogs).

## Quick start

```bash
task dev DB=sqlite   # build and start on a local file, nothing else needed
# or directly:
go run ./cmd/main.go
```

Then open <http://localhost:8000>. The web interface is served by the same
binary as the API.

`task dev` on its own adds PostgreSQL and a seeded test directory in
containers; `task stage` runs the real deployment image against them. See
[Development](#development).

**Installer.** With no database configured — no `DB_DIALECT` and no
`configs/datasource.json` — the binary does not start the application. It serves
an installer on the same port and waits, because there is nowhere to put an
account, a project or an hour until a database exists.

The connection is tested before it is accepted, written to
`configs/datasource.json`, and then the application takes over the same port **in
the same process**. No restart: a container that exited to finish installing
itself would look like one that crashed.

It asks for a token, printed to the log when the process starts:

```
no database is configured, so Time Recording is serving its installer instead
open http://localhost:8000 to choose one - the application will not start until you do
setup token: 782106f2d715eaebba8e1c4b93f0a2d1
```

Until a database exists there is no account to authenticate against, and whoever
completes that screen decides where the installation keeps its data — so the one
thing standing between an exposed port and that decision is a value only somebody
who can already see the process can read. Set `SETUP_TOKEN` to choose it yourself
and drive the screen unattended.

Setting `DB_DIALECT` skips the installer entirely, which is how a container
deployment configures itself; see [Deployment](#deployment). `task dev` sets it
too, so development never meets this screen.

**First sign-in.** An administrator account is created on first start:

| | |
| --- | --- |
| User | `admin@local` |
| Password | `changeme123` |

That account **cannot be deleted**, cannot be moved to a role without
administration rights, and is never authenticated against a directory — so an
installation can never lock itself out, and nobody who controls the directory
can take it over. Until the initial password is replaced the server refuses
everything else, including issuing API tokens.

**Setup wizard.** On first sign-in the built-in administrator is walked through
what an installation has to settle, one step at a time. Two are required — the
administrator password and the timezone. The instance title and the directory
connection can be skipped.

The database is deliberately **not** among them, and that is the reason the
installer exists rather than a third step here. Everything the wizard settles is
stored *in* the database, so choosing one at this point would point the
application at an empty one: the password change, the timezone and the title
would stay behind in the old database, and start-up would recreate the
administrator with the initial password above. An installation that looked
configured would then be reachable with a password anyone can look up — and
nobody would expect it, having set a real one minutes earlier.

**Guided tour.** On a first sign-in every user — not just the administrator —
is walked through the areas of the application: booking time, the calendar, the
overtime balance, projects, their account. Steps are dropped for anything their
role cannot reach, so nobody is shown a tab they do not have, and the highlight
points at the real control rather than a picture of one. It runs once and can
be restarted any time from *My account*; skipping counts as seen. "Seen" is
stored on the user, so a second device does not mean a second introduction.

**Live log.** The built-in administrator can read what the process has written,
under *Settings*: filter by level, search the text, and set how often it
refreshes in seconds — or pause it. Newest at the bottom, and it follows along
only while you are already scrolled there, so reading something does not get
yanked away on the next refresh.

It shows the framework's own output too — the request log, a failing statement,
what happened during the migrations — because it captures the process output
rather than wrapping a logger. Two things worth knowing: only what the log level
admits reaches it, so ticking `DEBUG` on an installation running at `WARN` shows
nothing and is not a fault — the level is set under *Settings → Logging, metrics
and tracing* and applies from the next start; and it is held in memory in a
fixed-size ring, so it
starts empty after a restart and is no substitute for collecting logs. The
capture makes the console output JSON even on a terminal, which is what a log
collector wants anyway — `task dev` renders readable lines back for the console.

**Version.** The build the process was compiled from is in the bottom-right
corner of every page, including the sign-in screen. "Which version is actually
running" is the first question of every support conversation, and guessing it from
a container tag is not an answer.

Whether a setup step is done is worked out from what is **actually configured**, not
from a record of the wizard having been shown. So a setting that is later
undone makes its step outstanding again, and dismissing the wizard settles the
optional steps only: it comes back on its own while anything required is
missing. That is why "Finish later" is safe to offer.

## What is included

| Area | Implementation |
| --- | --- |
| Storage | SQLite (pure Go, no cgo), PostgreSQL or MySQL - chosen in the installer, never defaulted |
| Concurrency | SQLite runs in write-ahead logging, so a save and a page load do not refuse each other |
| Schema | GoFr migrations, applied automatically at start-up |
| API | REST under `/api/v1`, documented at `/api-docs` |
| Web interface | Embedded via `go:embed`, vanilla JS with no build step |
| Access control | RBAC with roles administered at run time; bcrypt password hashes |
| Sign-in | Session cookies, optional passkeys and TOTP two-factor per user |
| Live log | The process log, filterable and searchable, for the built-in administrator |
| Version | The running build in the footer of every page |
| API access | Personal tokens, scoped by the owner's current role |
| Overtime | Balance per day and per period against a personal daily target |
| Calendar | Month view of where hours were booked |
| Background job | Cron sweep that submits time entries left open too long |
| Transport | Optional HTTPS with automatic Let's Encrypt certificates |
| Operations | Health and liveness endpoints, Prometheus metrics, tracing (all from GoFr), administered under *Settings* |

## Access control

Users sign in with their email address and password. What someone may do comes
from their **role**, and roles are administered at run time through the
interface: create them, set their permissions, delete them.

| Role | Purpose |
| --- | --- |
| `admin` | Everything. A system role: it cannot be deleted or stripped of permissions |
| `manager` | Runs projects, sees and approves everyone's time entries |
| `employee` | Books and submits their own time, reads shared projects |

Permissions are fine grained, for example `timesheets:read:own` against
`timesheets:read:all`, and `timesheets:approve` separately from writing.
Someone holding only the `:own` variant is restricted to their own data
server-side, even if they send a filter naming somebody else.

Permissions are Go constants, not database rows: each one is checked by a
specific line of code, and a permission that existed only in the database would
grant nothing. The role editor therefore offers exactly the permissions that
are actually enforced.

### Reports

Aggregate reports — what other people total up to — are visible **only to the
built-in administrator**. Everyone else sees only their own figures. This is
enforced by `reports:read`, which by default no other role holds.

## API access with personal tokens

Every user can issue their own tokens under **My account**:

```bash
curl -H "Authorization: Bearer gtr_xxxxx" https://your-host/api/v1/timesheets
# or
curl -H "X-API-Token: gtr_xxxxx" https://your-host/api/v1/timesheets
```

A token carries **no rights of its own**. Every request is evaluated against
the owner's role *at that moment*, so changing or revoking a role takes effect
on the very next call, and a token can never outrank the person it belongs to.

Only a hash of the token is stored; the value itself is shown once, at
creation, and cannot be recovered afterwards. Tokens can be given an expiry and
can be revoked at any time.

Interactive documentation is at **`/api-docs`**, and the OpenAPI specification
it renders is at `/openapi.json` — load that into Swagger UI, Postman or
Insomnia if you prefer those.

## Security

Implemented, and verified against a running instance:

- **Session cookies** are `HttpOnly` and `SameSite=Lax`, and `Secure` whenever
  the connection is HTTPS. Sessions live in the database, so a restart does not
  sign everyone out. Changing a password ends every session of that user.
- **CSRF protection** on every request that changes something. Two independent
  checks have to pass: the `Origin` (or, failing that, the `Referer`) must name
  this host, and the `X-CSRF-Token` header must equal the `gtr_csrf` cookie.
  That cookie is deliberately readable by JavaScript — the point is that our own
  script can read it and another site's cannot. The token is replaced on
  sign-in, so one handed to an anonymous visitor never carries into a session.
  Requests authenticated by a personal API token are exempt, because a browser
  never attaches that header by itself and there is nothing to forge.
- **Passkeys** (WebAuthn) are opt-in per user, under *My account*. Signing in
  is then a fingerprint, a face or a device PIN - no password typed, so nothing
  to phish, reuse or read out of somebody else's breach. The private half never
  leaves the device; what is stored here is a public key that verifies
  signatures and cannot produce them.

  They are an **addition**, never a replacement: the password keeps working
  alongside, so a lost device is an inconvenience rather than a lockout. The
  built-in administrator is deliberately excluded from registering one at all -
  it exists so an installation always has a way back in, and a way back in that
  depends on a particular phone still existing is not one.

  The relying party is derived from the request, so nothing needs configuring.
  Two consequences worth knowing: passkeys are offered only over HTTPS or on
  `localhost` (browsers refuse elsewhere, and reject an IP address as a relying
  party outright), and a credential is bound to that domain permanently -
  moving the application to a different one means everyone re-registers.
- **Two-factor authentication** (TOTP, RFC 6238) is opt-in per user. The
  implementation is checked against the RFC's published test vectors, so it
  interoperates with real authenticator apps.

  **With both enabled, a passkey sign-in does not ask for a code.** That is
  deliberate rather than an oversight: registration and sign-in both require user
  verification, so the device had to see a fingerprint or a PIN before it would
  sign — possession of the device plus verification of the person, which is
  already two factors. Google, Microsoft and Apple treat passkeys the same way:
  they *satisfy* multi-factor rather than needing another one stacked on top.

  The consequence to be aware of: turning two-factor on does **not** force a
  second factor on somebody who has a passkey, because their passkey is a way in
  that never asks for one. If you need two-factor as an enforceable policy, this
  setting alone is not it. A browser test pins the behaviour, so changing it
  would have to be a decision rather than an accident.
- **Security headers** on every response: a strict `Content-Security-Policy`
  (no external origin is permitted, which nothing legitimate needs here),
  `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: no-referrer`, `Permissions-Policy`, and the two
  `Cross-Origin-*` isolation headers. API responses carry `Cache-Control:
  no-store`, because they contain personal data.
- **HSTS** is sent only over connections that already are HTTPS. Sending it
  over plain HTTP could lock a host out of working at all.
- **Rate limiting** on sign-in attempts and token-authenticated requests
  (default 30 per minute per client), which is what makes password and token
  guessing impractical. Ordinary browsing is not limited. A reverse proxy is
  still the right place for a hard global limit — this one is per process.
- **Secrets are never returned.** Database and LDAP passwords, and API tokens,
  are write-only over the API; clients get a `hasPassword` flag instead.
- **Enumeration is avoided**: a wrong password and an unknown account fail
  identically, and someone else's private project answers `404` rather than
  `403`, which would confirm the id exists.
- **Passwords** are bcrypt hashes; session and API tokens are 256 bits of
  randomness stored only as SHA-256.

### HTTPS with Let's Encrypt

Set `TLS_ENABLED=true` and list the hostnames in `TLS_DOMAINS`. Certificates
are obtained and renewed automatically, and cached under `TLS_CACHE_DIR` so a
restart does not request new ones.

Requirements: the host must be reachable from the internet on
`TLS_REDIRECT_PORT` (80), which answers the ACME challenge and redirects
everything else to HTTPS. Use `TLS_STAGING=true` while setting things up — its
certificates are not trusted by browsers, but its rate limits are far looser
than the production ones.

GoFr owns its own listener and accepts only a static certificate pair, so TLS
is terminated in front of it and proxied to localhost.

## LDAP

The administrator configures a directory under **Settings**. When it is
enabled, passwords are checked against the directory by binding as the user.

Accounts are also created on first successful sign-in, so someone can start
working without being provisioned first. Local-only accounts keep working
alongside directory ones. Roles and permissions always stay local — the
directory decides *who you are*, this application decides *what you may do*.

### Synchronisation

Under **Settings → Directory synchronisation** the whole directory is
reconciled with the local accounts:

- Accounts the directory has and this installation does not are **created**.
- Accounts the directory no longer holds are **deleted**, together with their
  time entries, private projects, API tokens and sessions.

**The directory is only ever read.** Nothing is written back to LDAP.

Set `LDAP_SYNC_SCHEDULE` to run it automatically; it is empty by default
because a run destroys recorded work irreversibly. Whatever the schedule, an
administrator can start a run by hand — and should use **Preview** first, which
reports exactly which accounts would go and how many time entries each one
would take with it. The real run asks for confirmation naming those numbers.

Four guards make a broken directory answer non-destructive:

| Guard | Reason |
| --- | --- |
| A failed directory read aborts with an error | An outage must never be read as "everybody left" |
| An empty result deletes nothing | Almost always a wrong base DN or filter, not a mass departure |
| `LDAP_SYNC_MAX_DELETE_RATIO` (default 0.5) | A truncated or misfiltered answer would otherwise look like half the company leaving |
| Local and built-in accounts are never touched | They were never in the directory, so its silence says nothing about them |

Listing is paged, because directories commonly cap a plain search at 500 or
1000 entries — a silent truncation would read as "everyone beyond the first
page has left".

## Overtime

Every user has a **daily target** (the basis for overtime) and a **daily
maximum** (the booking limit); both are set under *My account*. Without a
personal setting, 8 h target and the instance-wide `MAX_DAILY_HOURS` apply.

The balance is the sum of `booked − target` over the days that **have
bookings**. Days without bookings deliberately do not count: without a holiday
and leave calendar — which this application does not have — weekends and time
off would otherwise accumulate as a growing deficit. Rejected entries are
excluded.

## Timezones

An administrator sets one **instance-wide** zone under *Settings*; an
individual can override it under *My account*. An empty personal setting means
"follow the instance", which is the normal case — that way changing the
instance zone moves everyone who has not deliberately opted out.

This is not a display detail. The zone decides which **calendar day** a booking
falls on, so getting it wrong moves recorded hours between days and quietly
changes month-end totals. Zones are validated when saved, because a plausible
but wrong name like `Europe/Munich` would otherwise be stored and then fall
back to UTC at every use, with nothing on screen to show it.

The tz database is compiled into the binary (`time/tzdata`), so zones resolve
identically on a scratch or distroless container with no zoneinfo files of its
own.

## Projects

Projects are optional on a time entry: hours can be recorded first and
categorised later, or left uncategorised.

Beyond the shared projects, **every user can create their own private
projects** to split up a day when no shared project fits. A private project is
visible only to its owner — not to managers, and not to the administrator.
Creating one needs only `projects:write:own`, which every default role holds;
creating a *shared* project still needs `projects:write`.

## Architecture

Four layers; dependencies point inwards only.

```
cmd/main.go                     Wiring (DI), migrations, cron, TLS
│
├── internal/interface/         Entry points
│   ├── api/v1/rest/              HTTP handlers, DTOs, authorization, status codes
│   ├── web/                      Embedded web interface and its middleware
│   └── worker/                   Scheduled background jobs
│
├── internal/application/v1/    Use cases (CQRS-flavoured)
│   ├── command/ query/           Input and output per use case
│   ├── common/                   Result models and mappers
│   └── service/                  Orchestration and use-case rules
│
├── internal/domain/            Business core, free of framework code
│   ├── model/                    Entities, statuses, permissions
│   ├── repository/               Repository interfaces
│   └── service/                  Rules spanning several entities
│
└── internal/infrastructure/    Technical concerns
    ├── config/                   Application settings and the datasource file
    ├── directory/                LDAP client
    ├── tlsserver/                Let's Encrypt termination
    └── persistence/
        ├── sqldb/                Repositories, dialect-agnostic
        ├── memory/               In-memory repositories for tests
        └── migrations/           Schema definition
```

Decisions that would otherwise be surprising:

- **Routes are declared explicitly**, not through GoFr's `AddRESTHandlers`.
  That helper reflects over a struct to generate CRUD straight against a table,
  which would bypass the domain and application layers this project is built on.
- **The UI is served by custom middleware**, not GoFr's `AddStaticFiles`, which
  only reads from disk and would defeat the single-binary goal.
- **Authorization sits in the handlers**, not in a route table: several rules
  depend on the resource rather than the path — reading your own time entries
  and reading everyone's share one route.
- **The database connection is stored in a file**, not in the database. It
  describes *which* database to open, so keeping it inside that database would
  make it impossible to point the application elsewhere.

## Configuration

GoFr reads `configs/.env` first, then overlays `configs/.$APP_ENV.env` on top
of it. So `.env` holds every default, and the environment files beside it hold
only what actually differs — `.dev.env` is one line, `.prod.env` is none.

Four layers, lowest to highest:

| | Source | Set by |
| --- | --- | --- |
| 1 | `configs/.env` | the repository — every default |
| 2 | `configs/.$APP_ENV.env` | the repository — differences per environment |
| 3 | real environment variables | the deployment: compose, systemd, the shell |
| 4 | `configs/datasource.json` | the **setup wizard**, when someone changes the database in the interface |

Layer 3 is why a deployment needs no file edits: `deploy/compose.yaml` sets
everything it needs as environment variables. Layer 4 is deliberately on top of
it — changing the database in the interface is an explicit act, and it would be
surprising for it to be silently ignored because a stale variable was still set
somewhere.

With `APP_ENV` unset, layer 2 falls back to `configs/.local.env`. That file is
not in the repository and is gitignored: it is the right place for a personal
database or a debug log level, and the wrong thing to commit, because it would
silently apply to everyone.

### What belongs in a file, and what belongs in the application

**Bootstrap** settings can only be set in layers 1–3. They decide how the
process starts, so an application that has not started cannot administer them —
and getting one wrong must not be fixable only from a screen it takes away:
ports, `TLS_*`, `AUTH_ENABLED`, `UI_ENABLED`, and the `*_SCHEDULE` cron
expressions.

**Starting values** are what a fresh installation begins with; the setup wizard
and *Settings* administer them at run time and what is stored there wins:
`SESSION_LIFETIME`, `MAX_DAILY_HOURS`, `RATE_LIMIT`, `RATE_LIMIT_WINDOW`,
`AUTO_CLOSE_AFTER_DAYS`, `LDAP_SYNC_MAX_DELETE_RATIO`, `APP_NAME`.

**At the next start** are administered too, but stored rather than applied,
because GoFr reads them while it starts up: the `DB_*` connection, `LOG_LEVEL`,
`LDAP_SYNC_SCHEDULE`, and `TRACE_EXPORTER`, `TRACER_URL` and `TRACER_RATIO`. What
is stored wins from the next start onwards, and *Settings → Restart* lists what is
still waiting.

The **timezone and the LDAP connection appear in no file at all**. Both are
administered entirely in the application — a second place to write them would
only disagree with the first.

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_NAME` | `Time Recording` | instance title, until one is set under Settings |
| `HTTP_PORT` | `8000` | API and web interface |
| `METRICS_PORT` | `2121` | Prometheus endpoint; `0` switches it off |
| `DB_DIALECT` | – | `sqlite`, `postgres` or `mysql`. **Empty serves the installer** |
| `DB_NAME` | – | with SQLite, the file name without `.db` |
| `SETUP_TOKEN` | generated | what the installer asks for; logged when generated |
| `UI_ENABLED` | `true` | `false` runs the binary as a headless API |
| `AUTH_ENABLED` | `true` | `false` gives **every** caller full admin rights |
| `SESSION_LIFETIME` | `12h` | how long a sign-in stays valid |
| `TLS_ENABLED` | `false` | HTTPS with Let's Encrypt |
| `TLS_DOMAINS` | – | comma separated; required when TLS is on |
| `TLS_EMAIL` | – | receives expiry warnings |
| `TLS_PORT` / `TLS_REDIRECT_PORT` | `443` / `80` | HTTPS, and the ACME/redirect listener |
| `TLS_STAGING` | `false` | Let's Encrypt test authority |
| `HSTS_MAX_AGE` | `8760h` | only sent over HTTPS |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW` | `30` / `1m` | sign-in and token requests per client |
| `LDAP_SYNC_SCHEDULE` | empty | cron for the directory reconciliation; empty means manual only. Administered under *Settings* as well, where what is saved wins from the next start |
| `LDAP_SYNC_MAX_DELETE_RATIO` | `0.5` | refuse a run removing more than this share of directory accounts |
| `AUTO_CLOSE_SCHEDULE` | `0 2 * * *` | cron for the sweep; empty disables it |
| `AUTO_CLOSE_AFTER_DAYS` | `14` | when an open entry gets submitted |
| `MAX_DAILY_HOURS` | `24` | instance-wide cap per person per day |
| `LOG_LEVEL` | `INFO` | `DEBUG`…`FATAL`; anything else is read as `INFO` |
| `TRACE_EXPORTER` | empty | `otlp` or `jaeger`; empty exports nothing |
| `TRACER_URL` | – | the collector as `host:port`, **without** a scheme |
| `TRACER_RATIO` | `1` | share of traces recorded, `0`–`1` |

### What can be changed from the interface, and what cannot

Six of the values above are **operational limits** rather than deployment
facts, so *Settings → Operation and limits* overrides them while the
application runs — no restart, no file access. A field left empty keeps
following the file, and its value is shown as the placeholder, so it is always
visible what a blank field means. The screen also prints what is currently in
force, and *Reset* drops every override at once.

| Administered from the interface | Applies |
| --- | --- |
| `SESSION_LIFETIME` | to the next sign-in |
| `MAX_DAILY_HOURS` | to the next booking |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW` | within seconds |
| `AUTO_CLOSE_AFTER_DAYS` | at the next sweep |
| `LDAP_SYNC_MAX_DELETE_RATIO` | at the next synchronisation |

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
| `*_SCHEDULE` | cron jobs are registered once at start-up and cannot be re-registered live |
| `HTTP_PORT`, `METRICS_PORT` | bound at start-up — and GoFr refuses to start when something already holds the metrics port, so a port saved from a screen could stop the application together with the screen. *Settings* can only switch the endpoint **off**, which cannot fail |

`APP_NAME` is not administered here either — the instance title under
*Settings → Appearance* already overrides it, and two fields for one label
would only disagree.

For PostgreSQL also set `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD` and
`DB_SSL_MODE` — or configure them under **Settings**, where a *Test connection*
button probes them before you commit. A connection saved there is written to
`configs/datasource.json` and applied on the next restart; switching a live
database under running requests is not safe, so it is deliberately not done.

*Settings → Logging, metrics and tracing* works the same way, and for the same
kind of reason: GoFr reads the log level, binds the metrics port and builds the
trace exporter inside `gofr.New()`, so nothing administered afterwards could
reach any of them. GoFr can change a running logger's level, but it does so by
assigning to a field every request goroutine reads without synchronisation — a
data race is not a reasonable price for saving a restart. What is
saved there is stored in the database and read back out of it on the way into the
next start, before GoFr reads its own configuration — which is what lets a stored
value win over the file, including a stored *off*. A field left following the
configuration file keeps coming from there.

The screen shows what the running process is actually doing beside what is
stored, because until the next restart those disagree, and it names the metrics
endpoint in full so it can be copied. A *Restart* card lists what is waiting —
each setting with the value in force and the one that will replace it — and
offers to restart there and then, with the interface waiting for the application
to come back rather than leaving anyone to guess.

That restart replaces the process image rather than exiting and hoping something
starts it again. Exiting works under Docker with a restart policy and under
systemd with `Restart=`, and turns the button into an off switch everywhere else,
including a binary started by hand. `execve` needs nothing outside the process,
so there is no arrangement in which pressing it leaves the installation down.
Windows has no `execve`, so the button is not offered there and the screen says
why. Two exporters are offered, `otlp` and
`jaeger`. Zipkin is not: GoFr still accepts it while warning that it is on its
way out. Neither is GoFr's hosted exporter, which posts every span to a service
run by the framework's authors — not a thing to be able to switch on by picking
an entry from a list.

## API

Responses are wrapped by the framework: `{"data": ...}` or `{"error": ...}`.
The full reference is at `/api-docs`; the highlights:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/login`, `/auth/logout` | Sign in and out |
| `GET` | `/api/v1/me` | Own identity and permissions |
| `PUT` | `/api/v1/me/password`, `/me/language` | Own password, own language |
| `POST/PUT/DELETE` | `/api/v1/me/totp` | Two-factor enrolment |
| `GET/POST` | `/api/v1/me/tokens` | Personal API tokens |
| `GET/POST/PUT/DELETE` | `/api/v1/users`, `/users/{id}` | User administration |
| `PUT` | `/api/v1/users/{id}/role`, `/working-times` | Role, daily target and maximum |
| `GET` | `/api/v1/users/{id}/overtime`, `/overtime` | Own and team overtime |
| `GET/POST/PUT/DELETE` | `/api/v1/roles`, `/roles/{id}` | Roles |
| `GET` | `/api/v1/permissions` | Every enforced permission |
| `GET/POST/PUT/DELETE` | `/api/v1/projects`, `/projects/{id}` | Projects |
| `POST` | `/api/v1/projects/{id}/archive` | Archive |
| `GET` | `/api/v1/projects/{id}/report` | Report |
| `GET/POST/PUT/DELETE` | `/api/v1/timesheets`, `/timesheets/{id}` | Time entries |
| `POST` | `/api/v1/timesheets/{id}/transfer` | Move to another project |
| `GET/PUT` | `/api/v1/settings/...` | Branding, database, LDAP, metrics and tracing |

### What the metrics endpoint carries

GoFr measures the machinery on its own — a histogram per HTTP request
(`app_http_response`), one per SQL query (`app_sql_stats`), a goroutine gauge —
and it does so without a line of instrumentation in this repository. Spans are
the same: every request is traced by the framework's middleware, which is why
tracing works here with no span code anywhere.

What it cannot know is whether the application is doing its job. A deployment can
serve every request in milliseconds while nobody has been able to book time since
the directory changed. So four more are recorded here, each because somebody
would act on it:

| Metric | Says |
| --- | --- |
| `gtr_timesheet_hours_booked` | hours per entry — the sum is what was recorded, the count in how many pieces |
| `gtr_timesheet_transitions_total` | entries entering a state, by state — a queue of submitted entries nobody approves is invisible otherwise |
| `gtr_signin_failures_total` | refused sign-ins, by reason — `credentials` is somebody guessing, `directory` is a directory that stopped answering |
| `gtr_directory_accounts_total` | accounts the synchronisation created or deleted — the one operation that removes people together with their hours |

None of them carries a user, an address or a project name as a label. A label is
a time series: one per person is both a memory leak in the collector and a list
of who works here, published on a port that asks for no password.

**Before writing an alert:** a metric is published only once it has a value, so
an installation that has had no refused sign-in publishes no
`gtr_signin_failures_total` at all rather than publishing it as zero. Treat an
absent series as absent — `absent()` — rather than as a healthy zero.

Operations: `/.well-known/health`, `/.well-known/alive`, and `/metrics` on port
2121 — a port of its own, outside the middleware chain, which therefore asks for
no sign-in, is not covered by TLS, and serves Go's profiling endpoints under
`/debug/pprof/` beside the metrics. Reach it from your monitoring, not from the
internet, or switch it off under *Settings*.

## Business rules

The server enforces these; the interface merely also hides what is not allowed:

- Time entries follow `open → submitted → approved / rejected`; rejected may go
  back to `open`. Other jumps are refused.
- An **approved** entry can no longer be changed, deleted or moved.
- Approving and rejecting needs `timesheets:approve` — someone who may only
  write their own time can submit it, but not approve it.
- Hours are booked only onto **active** projects.
- The personal daily maximum applies, falling back to `MAX_DAILY_HOURS`.
- Archiving requires a completed project with no open entries left.
- A project that still has time entries cannot be deleted.
- Email addresses are unique.
- The built-in administrator cannot be deleted or stripped of administration.
- System roles cannot be deleted, renamed or weakened.
- A role still assigned to someone cannot be deleted.
- A private project is invisible to everyone but its owner.

## Development

| Task | What it does |
| --- | --- |
| `task dev` | **Develop.** Backing services, then the locally built binary against them, on :8000 |
| `task test` | Unit and integration tests |
| `task stage` | **Verify.** The shipped container image against real services, on :8080 |
| `task image` | **Ship.** Build the deployment image |
| `task release VERSION=v1.2.0` | **Ship.** Tag a minor or major version by hand; a patch needs no command at all |

`task` on its own lists everything, and `task --summary <name>` explains one.

### Develop

```bash
task dev                 # PostgreSQL + seeded directory, then the app
task dev DB=sqlite       # no containers at all, straight onto a local file
task env:down            # stop everything and delete the data
```

The application runs in the foreground; `Ctrl-C` stops it and leaves the
containers up, so the next start is quick. LDAP is not configured through the
environment — it is administered in the running application under *Settings*;
[`test/README.md`](test/README.md) lists the values and the seeded accounts
that make the synchronisation's edge cases reproducible.

### Verify

```bash
task test                # unit tests
task test:integration    # the real binary, driven over HTTP, end to end
task test:browser        # the real interface, driven in a real browser
task stage               # the real image, against real services, on :8080
task stage:logs          # follow its log
task stage:down          # stop it and delete the data
```

**Integration tests** ([`test/integration/`](test/integration/)) start the
compiled binary and talk to it over HTTP, the way a browser and a script do.
Nothing is stubbed, so the middleware order, the CSRF check, the session
cookie, the migrations and the embedded assets are all in the path — which is
where every bug found by *running* this application rather than testing it has
lived. They cover sign-in, the setup wizard, booking and approval, the daily
cap, overtime, private projects, RBAC, API tokens, timezones and the guided
tour.

```bash
task test:integration              # against SQLite, one file per test
task test:integration DB=postgres  # against the dialect production runs on
task test:integration DB=mysql     # and the third one
```

**Browser tests** ([`test/browser/`](test/browser/)) click through the real
interface with Chrome, Chromium or Edge. They cover what no API test can:
whether the application is *usable*. A sign-in overlay that never goes away, a
tab that highlights but changes nothing, a stylesheet rule that quietly beats
the `hidden` attribute — every one of those leaves the API perfectly healthy
and the application unusable. This project shipped exactly that once.

```bash
task test:browser
```

Or all three at once, in the order that fails fastest — a compile error should
not be found after twenty minutes of browser automation:

```bash
task test:all              # unit, then integration, then browser
task test:all DB=postgres  # with the integration leg against PostgreSQL
```

CI runs all of it on every push and before every release: unit tests with
`-race`, integration against **all three dialects**, and the browser suite.

`task stage` is what `task dev` is not. Development runs a binary compiled on
your machine — fast, but not the artifact anyone deploys. Staging goes through
the repository `Dockerfile`: multi-stage build, embedded assets, non-root user,
healthcheck, `prod` configuration. It publishes on **8080** rather than 8000 so
a development instance can keep running beside it.

```bash
# Check a connection with the application's own drivers, before trusting it
task probe -- --ldap ldaps://dc.corp.example --id-attribute objectGUID
```

Formatting, vet and linting deliberately do not run through the Taskfile: the
VS Code Go extension covers them locally, and
[`ci.yml`](.github/workflows/ci.yml) enforces them independently.

```bash
golangci-lint run ./...   # configured in .golangci.yml
```

CI runs the version named in
[`.golangci-lint-version`](.golangci-lint-version) rather than whatever is
newest, so a push cannot fail on a commit that passed yesterday because a new
release added a check. Keep the locally installed version in step with that
file — `golangci-lint --version` — or local runs and CI will disagree about
clean.

## Deployment

**Every merge to `main` is a release.** The patch number goes up, the image is
published to GHCR as both that version and `:latest`, and a GitHub release is
created with generated notes. The version is not written down anywhere:
[`release.yml`](.github/workflows/release.yml) reads the newest tag and counts on,
so there is no file to forget to bump and no way for a tag and a constant to
disagree. With no tags at all it starts at `v0.1.0`.

For a minor or major bump, tag it by hand: `task release VERSION=v1.2.0` pushes
the tag and that exact version is released — the next merge then counts on from
there.

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

### On the server

Copy [`deploy/`](deploy/) — two compose files and an environment template — and
nothing else. The source tree is not needed.

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

Pin `GTR_VERSION` to a release rather than leaving it on `latest`. A container
restarted at 3am otherwise comes back as a different version than the one that
went down.

The compose file sets `DB_DIALECT`, so a server deployment never meets the
installer — it is configured before it starts, which is what an unattended
deployment needs. Leaving `DB_DIALECT` out is what turns the installer on, and a
container waiting for somebody to click something is rarely what you want on a
server; if you do, set `SETUP_TOKEN` so the token is not something you have to go
and read out of `docker logs`.

### HTTPS

With a reverse proxy already terminating TLS, there is nothing more to do. To
terminate it in the application instead, add the overlay:

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

### Updating

```bash
docker compose pull && docker compose up -d
```

Schema migrations run at start-up, so there is no separate step. Back the
database up first — the application will not undo a migration for you:

```bash
docker compose exec -T postgres pg_dump -U "$DB_USER" "$DB_NAME" | gzip > backup-$(date +%F).sql.gz
```

### Configuration

`.env` carries only what a deployment must decide: the database password, the
image version, the instance name, and the TLS settings. Everything operational —
session lifetime, rate limits, the daily booking cap, the directory connection,
the timezone — is administered in the running application under *Settings*, and
takes effect without a restart. See [Configuration](#configuration) for which
settings live where and why.

### Running it without containers

The binary is self-contained; the image is a convenience, not a requirement.

```bash
DB_DIALECT=postgres DB_HOST=… DB_USER=… DB_PASSWORD=… ./go-time-recording
```

It has to be started from a directory containing `configs/`, which is where
GoFr looks for its configuration.

## Licence

MIT.
