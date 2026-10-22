<h1 align="center"><img src="internal/interface/web/assets/favicon.svg" alt="" width="96"><br>Go-Time-Recording</h1>

<p align="center">
  <b>Project time tracking in a single self-contained binary.</b><br>
  A web interface and a REST API for recording where working hours go —
  self-hosted, on SQLite, PostgreSQL or MySQL.
</p>

<p align="center">
  <a href="https://github.com/dennis-dko/go-time-recording/actions/workflows/ci.yml"><img src="https://github.com/dennis-dko/go-time-recording/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/dennis-dko/go-time-recording/releases/latest"><img src="https://img.shields.io/github/v/release/dennis-dko/go-time-recording?label=release" alt="Latest release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/dennis-dko/go-time-recording" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/dennis-dko/go-time-recording" alt="MIT licence"></a>
</p>

<p align="center">
  <a href="#get-started">Get started</a> ·
  <a href="#a-tour">A tour</a> ·
  <a href="#how-it-fits-together">How it fits together</a> ·
  <a href="#what-it-does">What it does</a> ·
  <a href="#documentation">Documentation</a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="doc/images/calendar-dark.png">
  <img src="doc/images/calendar.png" alt="The calendar: a month of booked hours, with two days of leave, a day off sick and a public holiday marked">
</picture>

## Why Go-Time-Recording

- 📦 **One file to run.** The interface, the API, the schema migrations and the
  timezone database are compiled into one binary — for Linux, Windows and macOS,
  and as an image for amd64 and arm64. Nothing to unpack beside it.
- 🔒 **Your hours stay yours.** Every project belongs to one person, and nobody
  else — the administrator included — can read, total or correct anybody's time.
  An installation may switch on managers for the people an administrator assigns
  them, and then those people are told who, and see their name on every entry a
  manager touched. No team totals, no approval queue.
- 🗄️ **The database you already run.** SQLite in a single file, or PostgreSQL or
  MySQL. Started without one, the binary serves an installer on its own port and
  waits.
- 🔑 **Signing in the way your organisation does.** Passwords with an
  authenticator app, passkeys, LDAP and Active Directory with Kerberos single
  sign-on, and OpenID Connect providers such as Keycloak, Authentik, Entra ID or
  Google.
- 📈 **Built to be operated.** Health endpoints, Prometheus metrics,
  OpenTelemetry traces, a live log, a maintenance mode, updates from the settings
  screen and HTTPS with Let's Encrypt.
- 🌍 **For the people using it.** English and German, light and dark, from a
  telephone to a wide screen — and installable as an app.

## A tour

Every picture below is taken from the running application by
[a test](test/browser/readmepictures_test.go) that books a year of somebody's
working time first — `task readme:pictures` takes them again.

<table>
  <tr>
    <td width="50%" valign="top">
      <img src="doc/images/week.png" alt="The week grid: projects as rows and days as columns, with the hours booked, what each day asked and the balance underneath">
      <p><b>The week as a grid.</b> Type hours straight into the cells; what each day asked and the balance sit underneath, and <i>Copy last week</i> fills the days that are still empty.</p>
    </td>
    <td width="50%" valign="top">
      <img src="doc/images/stopwatch.png" alt="The stopwatch, running for an hour and twenty minutes on the website relaunch">
      <p><b>A stopwatch that counts seconds.</b> Switch to the next project without losing one; a start pressed late can be corrected, and a clock left running is booked up to when the work ended.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/activities.png" alt="Charts of the month's hours per project and per activity">
      <p><b>Your own figures, as charts.</b> Hours per project, and per activity: write <code>#meeting</code> or <code>#review</code> into a description and it is counted — nothing to set up first.</p>
    </td>
    <td valign="top">
      <img src="doc/images/report.png" alt="A report of the month as a pie chart, one slice per project">
      <p><b>Reports in the shape the question needs.</b> Bars, columns or a pie, each project keeping its colour in all three, and a PDF of the same.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/year.png" alt="The year as tiles, one per day, darker for more hours, with public holidays and days away outlined">
      <p><b>The year at a glance.</b> A tile a day, as dark as the hours on it; public holidays and days away outlined rather than told by colour alone, beside the year before.</p>
    </td>
    <td valign="top">
      <img src="doc/images/projects.png" alt="The project list, one project at 86 % of its hour budget and one past it at 114 %">
      <p><b>Projects with hour budgets.</b> The list says how much of a budget is booked, marked from four fifths, and in red once it is past.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/overtime.png" alt="The overtime balance of the month, day by day">
      <p><b>An overtime balance against your own target.</b> Choose a day, and from then on every working day counts — less what a public holiday or a day away takes off it.</p>
    </td>
    <td valign="top">
      <img src="doc/images/workday.png" alt="The working time of the month: when each day began and ended, its breaks, and the hours booked beside them">
      <p><b>Your working time, on record.</b> When each day began and ended and how long the breaks were, beside the hours booked; a gap shows as one, and the month leaves as a PDF.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/entries.png" alt="The entry list narrowed to the month's meetings">
      <p><b>Every entry, findable.</b> Narrow the list by project, period or words in the description — the export follows the same filter — and every correction keeps the version it replaced.</p>
    </td>
    <td valign="top">
      <img src="doc/images/signin.png" alt="The sign-in form, offering a password, a passkey and the organisation's provider">
      <p><b>Sign in your way.</b> A password and an authenticator app, a passkey, your organisation's provider — or, inside a domain, the Kerberos ticket the browser already holds.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/log.png" alt="The live log under Settings, filtered by level">
      <p><b>Operated from the browser.</b> The process log, live; the database, the directory, telemetry, maintenance, updates and a restart, all under <i>Settings</i>.</p>
    </td>
    <td valign="top">
      <img src="doc/images/installer.png" alt="The installer asking for the setup token and a database">
      <p><b>Set up in the browser.</b> Without a database the binary serves this screen, guarded by a token from its log, and then takes its port over in the same process.</p>
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="doc/images/calendar-de.png" alt="The calendar in German">
      <p><b>English and German</b>, every screen and every refusal — dates and figures written the way the reader's language writes them.</p>
    </td>
    <td valign="top" align="center">
      <img src="doc/images/phone.png" alt="The stopwatch on a telephone" width="240">
      <p><b>From a telephone to a wide screen</b>, and installable as an app with the installation's own name and logo - which opens without a network, and keeps what is booked meanwhile until it is back.</p>
    </td>
  </tr>
</table>

## How it fits together

One process answers everybody: the browser, the scripts, the calendar apps.

```mermaid
flowchart TB
    people(["People<br/>browser · telephone"])
    scripts(["Scripts and tools<br/>personal API token"])
    calendars(["Calendar apps<br/>iCalendar subscription"])

    subgraph binary["go-time-recording — one binary"]
        installer["Installer<br/>until a database exists"]
        ui["Web interface<br/>embedded"]
        api["REST API<br/>/api/v1 · /api-docs"]
    end

    db[("SQLite · PostgreSQL · MySQL")]
    directory["LDAP · Active Directory<br/>Kerberos"]
    provider["OpenID Connect provider"]
    telemetry["Prometheus · OpenTelemetry"]

    people --> ui --> api
    scripts --> api
    calendars --> api
    installer -. chooses .-> db
    api --> db
    api -. sign-in .-> directory
    api -. sign-in .-> provider
    api -. metrics · traces .-> telemetry
```

Everything a person books ends up in one place, and every figure is read from
there:

```mermaid
flowchart LR
    stopwatch["Stopwatch"] --> entry
    form["Entry form"] --> entry
    grid["Week grid"] --> entry
    workbook["Spreadsheet import"] --> entry
    entry[("Time entry<br/>day · hours · project · activity")]
    entry --> calendar["Calendar and year"]
    entry --> figures["Charts and reports<br/>PDF · spreadsheet"]
    entry --> balance["Overtime balance"]
    entry --> feed["Calendar subscription"]
    plan["Planned week<br/>public holidays · days away"] --> balance
```

The first start, when nothing is configured yet:

```mermaid
sequenceDiagram
    actor You
    participant App as go-time-recording
    participant DB as Database
    You->>App: start it, with no database configured
    App-->>You: installer on :8000, setup token in the log
    You->>App: the token, and SQLite, PostgreSQL or MySQL
    App->>DB: test the connection, create the schema
    App-->>You: same port, same process — signed in, choose a password
```

## Get started

**With Docker Compose** — the application and a PostgreSQL beside it, from the
[`deploy/`](deploy/) folder:

```bash
cp .env.example .env    # DB_PASSWORD is required
docker compose up -d
```

**As a single binary** — download the one for your platform from the
[latest release](https://github.com/dennis-dko/go-time-recording/releases/latest)
and start it:

```bash
chmod +x go-time-recording_*_linux_amd64
./go-time-recording_*_linux_amd64
```

Then open <http://localhost:8000>. Without a database the binary serves the
installer, which asks for the setup token printed in its log; with one, sign in
as `admin@local` with the initial password `changeme123`, and the first thing
asked for is a new one. Windows warns about an unknown publisher the first time
— [here is why](doc/getting-started.md#windows-says-unknown-publisher).

**From the source**, with [Task](https://taskfile.dev):

```bash
task dev DB=sqlite      # or: go run ./cmd
```

Running it for real — behind a proxy, with HTTPS, as a service — is
[`deploy/OPERATIONS.md`](deploy/OPERATIONS.md).

## What it does

| Area | What you get | Read more |
| --- | --- | --- |
| **Recording** | Stopwatch, entry form and week grid; templates for what is booked often; changes to many entries at once; spreadsheet import and export | [Features](doc/features.md) |
| **History** | Every correction keeps the version it replaced, for the entry's owner | [Features](doc/features.md#business-rules) |
| **Evaluating** | Charts per day, project and activity, reports as bars, columns or a pie, the year as tiles, statements of work, PDF exports | [Features](doc/features.md) |
| **Projects** | Private to their owner, with a period, a status, an hour budget and an hourly rate | [Projects](doc/features.md#projects) |
| **Targets and time off** | A planned week, the public holidays of every German state, leave with its entitlement and carry-over, sick days and days taken against overtime | [Overtime](doc/features.md#overtime) |
| **Working time** | When each day began and ended and its breaks, for each person's own record | [Working time](doc/features.md#your-own-record-of-your-working-time) |
| **Calendar subscription** | Booked days and days away in Outlook, Google or a telephone's calendar | [Calendar](doc/features.md#your-days-in-your-own-calendar) |
| **Accounts and roles** | Roles and their rights administered at run time; an administrator who runs the installation without keeping hours in it; managers for the accounts assigned to them, where an installation switches them on | [Access](doc/access.md) |
| **Sign-in** | Passwords with TOTP, passkeys, LDAP and Active Directory with synchronisation, Kerberos, OpenID Connect | [Sign-in](doc/access.md#ldap) |
| **API** | REST under `/api/v1`, described at `/api-docs`, reached with personal tokens that never outrank their owner - and `go-time-recording track` books through it from a terminal | [API](doc/api.md) |
| **Operations** | Health, metrics, traces, live log, maintenance mode, updates, restart, HTTPS with Let's Encrypt, the installation's own name and logo | [Configuration](doc/configuration.md) |

## Documentation

| Page | What it covers |
| --- | --- |
| [Getting started](doc/getting-started.md) | The installer, the first sign-in, the setup wizard and the tour |
| [Features](doc/features.md) | Everything the application does, and the rules it keeps while doing it |
| [Accounts, roles and sign-in](doc/access.md) | Rights, the administrator, API tokens, LDAP, Kerberos and OpenID Connect |
| [Security](doc/security.md) | Sessions, CSRF, passkeys, two factors, HTTPS, and what a copy of the database gives away |
| [Configuration](doc/configuration.md) | Where each setting comes from, and what the interface can change |
| [API](doc/api.md) | What a refusal looks like, and what the metrics carry |
| [Architecture](doc/architecture.md) | The four layers, and the decisions that would otherwise surprise |
| [Development](doc/development.md) | The environments, and the checks a change has to pass |
| [Releases and deployment](doc/deployment.md) | How a release is made and what it carries |
| [Operations manual](deploy/OPERATIONS.md) | For whoever runs it: the four ways to deploy, backups, restarts, health |

Built on [GoFr](https://gofr.dev), structured after [gogs](https://github.com/gogs/gogs).

## License

[MIT](LICENSE).
