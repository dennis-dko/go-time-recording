# Go-Time-Recording

Project time tracking as **a single self-contained binary**: the REST API and
the web interface live in the same executable (the UI assets are embedded with
`go:embed`), and the database is SQLite by default. Starting it needs no
database server, no asset directory and no migration step.

Built on [GoFr](https://gofr.dev), structured after [gogs](https://github.com/gogs/gogs).

## Quick start

```bash
task run          # build and start (defaults to the dev environment)
# or directly:
go run ./cmd/main.go
```

Then open <http://localhost:8000>. The web interface is served by the same
binary as the API.

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
what an installation has to settle, one step at a time. Three are required —
**the database first**, then the administrator password and the timezone. The
instance title and the directory connection can be skipped.

The database comes first because everything else is stored *in* it. Switching
later points the application at an empty database: the password change, the
timezone and the title stay behind in the old one, and start-up recreates the
administrator with the initial password above. An installation that looked
configured would then be reachable with a password anyone can look up — and
nobody would expect it, having set a real one minutes earlier. Staying on
SQLite is a perfectly good answer and settles the step; what the wizard will
not accept is not having decided.

**Guided tour.** On a first sign-in every user — not just the administrator —
is walked through the areas of the application: booking time, the calendar, the
overtime balance, projects, their account. Steps are dropped for anything their
role cannot reach, so nobody is shown a tab they do not have, and the highlight
points at the real control rather than a picture of one. It runs once and can
be restarted any time from *My account*; skipping counts as seen. "Seen" is
stored on the user, so a second device does not mean a second introduction.

Whether a setup step is done is worked out from what is **actually configured**, not
from a record of the wizard having been shown. So a setting that is later
undone makes its step outstanding again, and dismissing the wizard settles the
optional steps only: it comes back on its own while anything required is
missing. That is why "Finish later" is safe to offer.

## What is included

| Area | Implementation |
| --- | --- |
| Storage | SQLite by default (pure Go, no cgo); switchable to PostgreSQL or MySQL |
| Schema | GoFr migrations, applied automatically at start-up |
| API | REST under `/api/v1`, documented at `/api-docs` |
| Web interface | Embedded via `go:embed`, vanilla JS with no build step |
| Access control | RBAC with roles administered at run time; bcrypt password hashes |
| Sign-in | Session cookies, optional TOTP two-factor per user |
| API access | Personal tokens, scoped by the owner's current role |
| Overtime | Balance per day and per period against a personal daily target |
| Calendar | Month view of where hours were booked |
| Background job | Cron sweep that submits time entries left open too long |
| Transport | Optional HTTPS with automatic Let's Encrypt certificates |
| Operations | Health and liveness endpoints, Prometheus metrics, tracing (all from GoFr) |

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
- **Two-factor authentication** (TOTP, RFC 6238) is opt-in per user. The
  implementation is checked against the RFC's published test vectors, so it
  interoperates with real authenticator apps.
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

Values come from `cmd/configs/.<env>.env`; `APP_ENV` selects the environment.

| Variable | Default | Meaning |
| --- | --- | --- |
| `HTTP_PORT` | `8000` | API and web interface |
| `METRICS_PORT` | `2121` | Prometheus endpoint |
| `DB_DIALECT` | `sqlite` | also `postgres`, `mysql` |
| `DB_NAME` | `go-time-recording` | with SQLite, the file name without `.db` |
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
| `LDAP_SYNC_SCHEDULE` | empty | cron for the directory reconciliation; empty means manual only |
| `LDAP_SYNC_MAX_DELETE_RATIO` | `0.5` | refuse a run removing more than this share of directory accounts |
| `AUTO_CLOSE_SCHEDULE` | `0 2 * * *` | cron for the sweep; empty disables it |
| `AUTO_CLOSE_AFTER_DAYS` | `14` | when an open entry gets submitted |
| `MAX_DAILY_HOURS` | `24` | instance-wide cap per person per day |

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
| `HTTP_PORT`, `METRICS_PORT` | bound at start-up |

`APP_NAME` is not administered here either — the instance title under
*Settings → Appearance* already overrides it, and two fields for one label
would only disagree.

For PostgreSQL also set `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD` and
`DB_SSL_MODE` — or configure them under **Settings**, where a *Test connection*
button probes them before you commit. A connection saved there is written to
`configs/datasource.json` and applied on the next restart; switching a live
database under running requests is not safe, so it is deliberately not done.

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
| `GET/PUT` | `/api/v1/settings/...` | Branding, database, LDAP |

Operations: `/.well-known/health`, `/.well-known/alive`, metrics on port 2121.

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

```bash
task            # build (dev)
task run        # build and start; ENV=staging and so on are possible
task test       # tests
task upgrade    # bump go.mod to the installed toolchain and update dependencies
task release VERSION=v1.2.3   # tags; CI does the rest
```

Formatting, vet and linting deliberately do not run through the Taskfile: the
VS Code Go extension covers them locally, and
[`ci.yml`](.github/workflows/ci.yml) enforces them independently.

```bash
golangci-lint run ./...   # configured in .golangci.yml
```

## Deployment

```bash
docker build -t go-time-recording .
docker run -p 8000:8000 -v go-time-data:/data go-time-recording
```

The image is built in two stages; the final layer holds only the static binary,
the configuration and a non-root user. The SQLite file lives under `/data` and
belongs on a volume, or it is gone when the container is replaced.

For HTTPS, publish ports 80 and 443 as well and set `TLS_ENABLED`,
`TLS_DOMAINS` and `TLS_EMAIL`. Mount `TLS_CACHE_DIR` on a volume too, so
certificates survive a restart and are not requested again.

On a `vX.Y.Z` tag, [`release.yml`](.github/workflows/release.yml) publishes the
image to GHCR and creates a GitHub release.

## Licence

MIT.
