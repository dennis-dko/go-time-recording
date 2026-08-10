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

**Welcome, and the guided tour.** A first sign-in is greeted by name, told in a
sentence what the application is for, and offered a walk through it. The list of
what you can do here is built from your own permissions, so nobody is promised
something they cannot do.

The walk itself covers booking time by hand and by stopwatch, the entry list and
the calendar and correcting an entry from it, your
own figures as charts, the overtime balance, projects including private ones,
reports, tokens and the second factor, and appearance and language. Steps are
dropped for anything your role cannot reach, so nobody is shown a tab they do not
have, and the highlight points at the real control rather than a picture of one.

It is offered once. Declining counts, the same way skipping does, and it can be
restarted any time from *My account*. "Seen" is stored on the user, so a second
device does not mean a second introduction.

The **built-in administrator is not greeted**: that account arrives at the setup
wizard, which is its own introduction, and a walk through booking time and reading
an overtime balance would be a walk through somebody else's job. The card under
*My account* still starts it on request.

On later visits there is a short **welcome back** instead, at the top of the first
screen: what today already has booked on it, or a warning that you left a stopwatch
running. Once per visit rather than per page load — a reload is not an arrival.

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
| Stopwatch | Start and stop the clock; the measured time becomes an entry, to the second |
| Own statistics | Hours per day and per project, drawn as charts, for everybody rather than administrators |
| Overtime | Balance per day and per period against a personal daily target |
| Calendar | Month view of where hours were booked |
| Transport | Optional HTTPS with automatic Let's Encrypt certificates |
| Operations | Health and liveness endpoints, Prometheus metrics, tracing (all from GoFr), administered under *Settings* |

## Access control

Users sign in with their email address and password. What someone may do comes
from their **role**, and roles are administered at run time through the
interface: create them, set their permissions, delete them.

| Role | Purpose |
| --- | --- |
| `admin` | The installation, its accounts and their roles — and nothing else. A system role: it cannot be deleted or stripped of permissions |
| `employee` | Keeps their own time, projects and calendar |
| `employee-admin` | Both: somebody who works here and also administers |

The third one is the answer to "somebody here needs to administer as well". It is
handed out by the built-in administrator, so holding both jobs is a decision somebody
made rather than something an account arrived with — and it is an ordinary account
gaining administration, never the built-in account gaining a working day.

Each role's description travels with it into the dropdown where somebody assigns it,
because the difference between `employee` and `employee-admin` is a difference you can
only infer from the name — and what it decides is whether that person can administer
the installation. The three that ship are translated; a role an installation writes for
itself shows whatever was typed, which is the only sensible answer for words this
application has never seen.

Permissions are fine grained — reading separately from writing, projects separately
from time — and every one of them is scoped to the person holding it. A filter naming
somebody else is refused rather than quietly answered with your own rows.

There is no wider variant. `timesheets:read:all` and `timesheets:write:all` used to
exist beside the `:own` ones, and they were the last of the manager: one opened
everybody's entries, balances, totals and exports, the other let an account book time
in a colleague's name. No role held either by the end, and the four screens that asked
"which person" had been narrowed to a dropdown with a single entry — so ticking one in
the role editor changed nothing anybody could see, while the API answered every
question about every colleague. A capability with no screen is a capability nobody
audits, so both are gone.

Permissions are Go constants, not database rows: each one is checked by a
specific line of code, and a permission that existed only in the database would
grant nothing. The role editor therefore offers exactly the permissions that
are actually enforced.

### The administrator does not work here

Running an installation and recording time in it are two different jobs, and the
`admin` role does only the first: accounts, roles, the database, the directory, the
backups, the log. It has **no** working day at all — it cannot book an hour, keep a
project, read a figure or set a daily target, its own included.

That is deliberate, and the reason is the account itself. Every installation has the
built-in administrator before anybody has chosen anything: it is how you get in, not
somebody's working day. Whoever does work here has an account of their own, and if
that person also administers, they are given the `employee-admin` role rather than
made to sign in twice.

It used to book and read its own hours "like anybody who works here", and that was
the wrong shape — an account nobody chose, quietly holding a working day nobody asked
it to have.

What it does own is the instance-wide default under *Settings*, which is what a new
account starts on. Each person changes their own from there, and nobody changes
anybody else's.

On upgrade, the working day is taken off the `admin` role and the `employee-admin`
role is created first, so an installation that had been using the built-in account for
both jobs has somewhere to move that person. Nobody is moved automatically: who works
here is not something a database can work out, and guessing would either invent a
colleague or take an administrator's hours away. The entries stay in the tables and
become reachable again the moment the combined role is assigned.

It is a wall rather than a default. The `admin` role's permissions cannot be changed
at all — neither given nor taken — and the role editor shows them as unavailable rather
than letting them be ticked and refusing the save.

It was the other way round: whoever may manage roles could widen the role they hold, on
the reasoning that somebody who administers roles can reach anything anyway. That
reasoning does not survive this arrangement. The built-in administrator configures the
installation and records no time, so a right added to its role would hand a working day
to the one account nobody chose — quietly, from the screen that administers roles.

The way past the wall is a decision about a colleague: give a person the
`employee-admin` role. Note what that does *not* buy them — it is an employee's rights
plus the administration, so they keep their own hours and still nobody else's.

### Reports

A project's report totals **your own** hours on it. Everyone sees their own figures
there and through **My statistics**, and nobody sees anybody else's.

Whether somebody may see another person's recorded time is not a permission question,
because no permission answers yes — not for a list of entries, not for the spreadsheet
export, not for a project's total and not for an overtime balance. Whose entry it is
decides, and that is not a choice anybody can be granted.

Two rights used to answer it, which is how two answers come to disagree. `reports:read`
covered the totals and belonged to the role that reviewed other people's hours; when
that role went, the right stayed, held by nobody and gating a whole screen, so the
report was unreachable on every installation while appearing to be somebody else's
business. `timesheets:read:all` covered everything else and outlived it by one release.
Both are withdrawn on upgrade, and a role that held one keeps the half of it that is
still a right — whoever could read everybody's time can read their own.

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

  Enrolment shows a **QR code** to scan, with the key to type folded away behind
  it for a machine with no camera and for when a code will not scan. The code is
  rendered by the binary as an inline SVG, so it needs no external service — the
  strict `Content-Security-Policy` already allows `data:` images. It is cleared
  from the screen when you leave it: the picture encodes the shared secret.

  **With both enabled, a passkey sign-in does not ask for a code.** That is
  deliberate rather than an oversight: registration and sign-in both require user
  verification, so the device had to see a fingerprint or a PIN before it would
  sign — possession of the device plus verification of the person, which is
  already two factors. Google, Microsoft and Apple treat passkeys the same way:
  they *satisfy* multi-factor rather than needing another one stacked on top.

  Concurrency note, because it bit: every write to an account used to go through
  an update of the whole row, so two settings changed at once meant the second
  reverted the first — recording the guided tour as seen erased a two-factor secret
  issued a moment earlier. The settings a person changes in passing now write their
  own column, and the two two-factor columns are written together as the one fact
  they are.

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
  identically, and somebody else's project answers `404` rather than `403`, which
  would confirm the id exists.
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
  time entries, projects, API tokens and sessions.

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

Every user has a **daily target** (the basis for overtime) and a **daily maximum**
(the booking limit); both are set under *My account*, by the person they are about and
by nobody else. Without a personal setting, 8 h target and the instance-wide
`MAX_DAILY_HOURS` apply.

The two ceilings are not alternatives: the **stricter** one holds. You may hold your
own day shorter than the installation allows, because that is your time; you cannot
raise it past what the installation allows, because that ceiling is configuration and
configuration is the administrator's.

The balance is the sum of `booked − target` over the days that **have
bookings**. Days without bookings deliberately do not count: without a holiday
and leave calendar — which this application does not have — weekends and time
off would otherwise accumulate as a growing deficit.

It is your own balance. Somebody else's is their recorded time, totalled, so it
takes the same right as reading their entries — and the team-wide overview that used
to sit beside it is gone, because comparing colleagues is the one thing this
arrangement says nobody does.

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

**A project belongs to one person.** Only its owner sees it, only its owner books
on it, and that includes the administrator — whose own projects are equally its own
and nobody else's. Two people working on the same thing therefore keep a project
each, with the same name if they like, and nothing adds them together, because
nothing is meant to.

There were two kinds until recently: a shared project everybody could see, and a
private category for organising your own day. The second is the only kind now, and
with it went the second right — `projects:write:own` and `projects:write` were two
rights for two kinds of project, so one of them would have granted nothing. Whoever
held the old one holds `projects:write`, which an upgrade takes care of.

A start date is optional, for the same reason: a project is one person's way of
organising their hours, not a plan somebody signed off.

## Spreadsheets

Every table that holds something goes out and comes back as a real **.xlsx**
workbook, from the tab it belongs to: time entries with the date, the person, the
project, the hours and the description; projects with their period and status;
accounts with their role and whether the password lives in the directory. Names rather than identifiers, because a column of user ids is not something
anybody can fill in by hand; hours as a number, so the column can be totalled in
Excel.

The column headings are written in the language the export was asked for, and a file
exported in any of them imports again: the heading row is skipped by position, and
translated values are recognised in every language.

Not comma-separated text. A CSV is what Excel mangles: the separator depends on the
machine's locale, dates are re-interpreted on opening, and a description containing
a semicolon quietly becomes two columns — none of it recoverable once somebody has
saved over the file.

The export follows the filters on screen and the same scoping the entry list uses,
so it can never show more than the screen did.

The import is **two steps, and all or nothing**:

1. *Check the file* reads it and shows every row — which would be written, which
   would not, and why, named by its line in the sheet so you know where to look. It
   writes nothing.
2. *Import* is only offered when every row can be written, and runs in one
   transaction.

A file half-imported leaves nobody able to say which half, or which entries came
from it, which is why a single refused row refuses the file. Every row goes through
the rules the API enforces — by calling them, not by restating them: the same
validation, the same daily ceiling (counting the file against itself, so forty rows
on one day are checked together), the same project visibility. A row naming somebody
else needs the right to book for others.

An import of time entries **creates**; it never updates or replaces, because there is
no way for it to know which existing entry a row was meant to be. Projects are
matched by name, so importing the same file twice changes nothing the second time.
Accounts are matched on the mail address and are **changed**, not created: a new
account needs a password, and a password that arrives in a spreadsheet is a password
that gets mailed around.

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
`LDAP_SYNC_MAX_DELETE_RATIO`, `APP_NAME`.

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
Windows has no `execve`, so the button is not offered there and the card stays on
screen to say why — with nothing pending it used to disappear, which left the one
screen that explains the limitation unreachable on the one platform that has it.
There, a saved setting takes effect when the application is next started the way
it was started. Two exporters are offered, `otlp` and
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
| `PUT` | `/api/v1/users/{id}/role` | Role |
| `PUT` | `/api/v1/users/{id}/working-times` | Your own daily target and maximum |
| `GET` | `/api/v1/users/{id}/overtime` | An overtime balance — your own, unless you may read everybody's |
| `GET/POST/PUT/DELETE` | `/api/v1/roles`, `/roles/{id}` | Roles |
| `GET` | `/api/v1/permissions` | Every enforced permission |
| `GET/POST/PUT/DELETE` | `/api/v1/projects`, `/projects/{id}` | Projects |
| `POST` | `/api/v1/projects/{id}/archive` | Archive |
| `GET` | `/api/v1/projects/{id}/report` | Report |
| `GET/POST/PUT/DELETE` | `/api/v1/timesheets`, `/timesheets/{id}` | Time entries |
| `GET/POST/DELETE` | `/api/v1/me/timer` | Own stopwatch: read, start, discard |
| `POST` | `/api/v1/me/timer/stop` | Stop it and book the measured time |
| `GET` | `/api/v1/me/statistics` | Own hours per day, per project and per state |
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

- A time entry belongs to whoever recorded it. Reading, editing, deleting,
  transferring and totalling it are refused to everybody else, with no exception and
  no right that grants one — including the account that administers the installation
  and including the combined role. There is no state to travel through and nobody to
  approve anything: entries were once `open → submitted → approved`, and the role that
  approved them is gone.
- No screen asks which person. The booking form, the entry filter, the calendar and
  the overtime form each had a dropdown of colleagues; every one of them held a single
  name by the end, and a question with one answer is not a question.
- Hours are booked only onto **active** projects, whichever way they get there:
  booking, transferring, or editing an entry onto one.
- The personal daily maximum applies, falling back to `MAX_DAILY_HOURS`.
- Archiving requires a completed project with no open entries left.
- A project that still has time entries cannot be deleted.
- Email addresses are unique.
- The built-in administrator cannot be deleted or stripped of administration.
- A system role cannot be deleted or renamed, and its permissions cannot be changed —
  in either direction. Its description can.
- A role has to grant at least one permission. One that grants nothing can be assigned,
  and whoever holds it signs in to an interface with nothing on it, which reads as a
  broken installation rather than as a decision.
- A role still assigned to someone cannot be deleted.
- A project is invisible to everyone but its owner, and asking for one that is not
  yours answers `404` rather than `403`.

## Development

| Task | What it does |
| --- | --- |
| `task dev` | **Develop.** Backing services, then the locally built binary against them, on :8000 |
| `task test` | Unit and integration tests |
| `task stage` | **Verify.** The shipped container image against real services, on :8080 |
| `task image` | **Ship.** Build the deployment image |
| `task release VERSION=v1.2.0` | **Ship.** Tag a minor or major version by hand; a patch needs no command at all |

`task` on its own lists everything, and `task --summary <name>` explains one.

### The three environments

They differ in three things and nothing else: which database, which
`configs/.$APP_ENV.env` overlay, and whether the thing being run is a locally
built binary or the image that ships.

| | Develop | Stage | Production |
| --- | --- | --- | --- |
| Command | `task dev` | `task stage` | `docker compose up -d` in `deploy/` |
| `APP_ENV` | unset, so `.local.env` | `staging` | `prod`, set by the image |
| Runs | binary built just now | the real image | the published image |
| Database | SQLite file, or PostgreSQL in a container | PostgreSQL in a container | yours, in the environment |
| Port | 8000 | 8080 | 8000, behind whatever you put in front |
| Data | thrown away by `task env:down` | thrown away by `task stage:down` | a volume that outlives the container |

Stage exists because it is the only one that exercises what is actually shipped:
the multi-stage build, the embedded assets, the non-root user, the healthcheck.
`task dev` is faster and skips all of it. A change that works in develop and
fails in stage has failed in the image, which is the artifact anybody deploys.

Production configuration is [`deploy/`](deploy/) — two compose files and an
environment template, and nothing from the source tree. See
[Deployment](#deployment).

### Trying the installer

`task dev` sets `DB_DIALECT`, so development never meets the installer. To see
what a first-time operator sees, start the binary with no database configured
anywhere:

```bash
task build
cd bin
rm -f configs/datasource.json          # if a previous run wrote one
DB_DIALECT= DB_NAME= ./go-time-recording
```

Both variables have to be **empty rather than absent**: the process inherits your
shell, and a `DB_DIALECT` left over from something else would send it straight
into the application. The log then prints the setup token and the URL. Choosing
SQLite there writes `configs/datasource.json` and the application takes over the
same port in the same process, with no restart.

The integration suite covers this path as well - see
[`install_test.go`](test/integration/install_test.go), which drives it through
`harness.StartUnconfigured`.

### Surviving a network that misbehaves

Multi-statement writes are transactional, so a connection lost half way through
leaves the database describing something nobody asked for: see
[`sqldb.base.withTx`](internal/infrastructure/persistence/sqldb/sqldb.go) and the
commit that introduced it. The directory dials with a bounded timeout, so an
unreachable LDAP server delays one sign-in rather than holding a request open.

GoFr's **circuit breaker** does not apply here, which is worth stating rather than
leaving somebody to look for it. It lives in `pkg/gofr/service` and guards
*outgoing HTTP calls* registered with `AddHTTPService` — it takes a `HealthURL`
and polls it. This application makes no outgoing HTTP calls at all: it talks to a
SQL database and an LDAP server, neither of which is HTTP, and the optional trace
exporter is gRPC and managed by the framework. Wiring one in would add a
configuration surface that protects nothing.

**Caching** is likewise deliberate rather than absent. Two things are cached, both
because they are read on nearly every request and both with the staleness written
down: the maintenance state for two seconds
([`maintenance.go`](internal/interface/api/v1/rest/maintenance.go)) and the
operational limits ([`limits_provider.go`](internal/application/v1/service/limits_provider.go)),
each invalidated on save so a change takes effect on the next request rather than
at the end of an interval. GoFr can bring Redis, and for an installation the size
this is built for that would mean one more service to run, back up and keep
reachable in exchange for queries that already answer in under a millisecond
against a local file.

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
lived. They cover sign-in, the setup wizard, booking, the daily cap, overtime,
projects belonging to one person, RBAC, API tokens, timezones, deleting several rows
at once and the guided tour.

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

**Which build is this?** The footer names the version and the platform it is
running on — `v1.2.0 (windows)`. The same version is published for four platforms
and they do not all behave alike, so the version alone does not answer the first
question of a support conversation.

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

**What a release carries.** The image on GHCR, and a binary for each of

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
lifetime, the rate limit — comes from the environment or from a `.env` beside the
binary. [`deploy/.env.example`](deploy/.env.example) lists every variable with its
default; the release notes point at it too, because that is the moment somebody
needs it. Settings the interface can change live in the database and are not read
from there.

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
