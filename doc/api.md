# API

Responses are wrapped by the framework: `{"data": ...}` or `{"error": ...}`.
The full reference is at `/api-docs`; the highlights:

## From a terminal

The terminal client, `go-time-recording-client`, books through this API as the
owner of a personal token, made under *My account*, API tokens. It is a binary
of its own, released beside the installation's, and it belongs to that release:
what it asks for and how it reads the answers - the routes, the fields, the
codes a refusal carries and the values the page writes its own way - are those
of the version it was built from. `go-time-recording-client --version` says
which that is, and it should be the version the installation shows at the foot
of its pages.

On its own, on a terminal, it opens a screen: the stopwatch ticking, today's
entries under it, and a form for each change. `s` starts the stopwatch, `w`
switches it to another project and books what has run, `x` stops it and books
what it measured - after asking - and `b` books hours by hand. `r` reads the day
again, which the screen also does every half minute, so a stopwatch stopped in
the browser stops here too; `esc` puts a form away and `q` closes the screen,
which leaves a line saying how the stopwatch was left.

<img src="images/terminal.png" alt="The client's screen in a terminal: the stopwatch running for an hour and twenty minutes on the website relaunch, and today's three entries under it" width="600">

Each change is a command as well, for a script, an alias or a screen reader -
which read lines, where the screen draws:

```bash
export GTR_URL=https://time.example.com GTR_TOKEN=gtr_...
go-time-recording-client
go-time-recording-client start Website -m "release notes"
go-time-recording-client status
go-time-recording-client switch Webshop
go-time-recording-client stop
go-time-recording-client book 1:30 consulting -m "call" -d 2026-10-21
go-time-recording-client today
```

A project is named as it is called, or by the start of its name, among the
active ones; hours are written 1.5, 1,5 or 1:30. The stopwatch it starts is the
one the browser shows, running on the server, so stopping it anywhere stops it
everywhere. The address and the token come from the environment rather than from
flags, because a token on a command line lands in the shell's history and in every
process listing. The installation's certificate is checked against the
authorities the operating system trusts, so one whose certificate comes from an
authority of its own needs that authority installed on the machine as well. It
exits with 0 when done, 1 when the installation refused or could not be reached -
in the installation's own words - and 2 for a command line it could not read,
which is what a script tells the two apart by. `go-time-recording-client help`
lists the commands, and so does the client on its own wherever its output is not
a terminal, so a script that runs it bare gets lines and status 2 rather than a
screen waiting for keys.

It speaks English or German. `--lang de` or `--lang en` may stand anywhere on
the line, `GTR_LANG` keeps one, and otherwise it is the language the account
chose in the browser, which the client asks the installation for before
anything else. Where the account chose none, it is the language the system is
set to - `LC_ALL`, `LC_MESSAGES` or `LANG`, and on Windows, which sets none of
them, the language of its own screens - and so is what the client says before
it knows whose token it holds: its help, and a missing address or token. A
language that is asked for and not had is refused with status 2; one the system
is set to is only a guess, and anything but German there is English. In German,
the client says what the installation refuses the way the German web interface
does - from the code and the values the refusal carries, so a day in it is
written 06.10.2026 - and keeps the installation's English only where the
interface has no sentence for it.

## What a refusal looks like

Every refusal names itself. The name is what something other than an English
reader can act on — the interface looks up a sentence in the reader's own
language, and a support conversation has a stable word for a thing rather than a
paraphrase of a message that has since been reworded.

```json
{
  "error": {
    "code": "probeFailed",
    "message": "the connection could not be established",
    "detail": "cannot reach the database: dial tcp 10.0.0.4:5432: connect: connection refused",
    "ref": "A7F3C2"
  }
}
```

| Field | Always? | What it is |
| --- | --- | --- |
| `code` | yes | Why, as a stable name. Never changes meaning. |
| `message` | yes | The same thing in English, for a client with no dictionary. |
| `values` | sometimes | What the sentence interpolated, kept apart so a translation can put them in its own word order. |
| `param` | field rejections | Which fields, by name, so a form can label them the way it labels everything else. |
| `detail` | failures from underneath | The original wording of whatever actually failed — a driver, a directory, a file system. Untranslatable by nature, so it is carried rather than shown. |
| `ref` | internal failures | The same string as in the log line. This is how a screenshot and a log entry are matched up. |

Two kinds of `code`. Specific ones are declared where the rule is enforced
(`projectHasEntries`, `timesheetLocked`). The generic ones — the refusals that
belong to no single rule — are declared once in
[`apperror/codes.go`](../internal/support/apperror/codes.go): `internal`, `probeFailed`,
`unauthenticated`, `notFound`, `invalidFields`, `rateLimited`, `csrfRejected`,
`maintenance`.

Words rather than numbers, deliberately. A numbered scheme is right where the
code is all you get — a return value in a register with a table somewhere else.
These travel in a JSON body with room beside them, so the property worth having
is that the code says what it means where it is read. What is borrowed from the
numbered systems is the part that makes them work: the set is closed and nothing
may emit a reason outside it, which
`TestEveryErrorTheAPICanGiveIsNamed` checks by provoking each one against a
running instance.

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
| `GET` | `/api/v1/me/statistics` | Own hours per day and per project |
| `POST` | `/api/v1/timesheets/{id}/transfer` | Move to another project |
| `GET/PUT` | `/api/v1/settings/...` | Branding, database, LDAP, metrics and tracing |

## What the metrics endpoint carries

GoFr measures the machinery on its own — a histogram per HTTP request
(`app_http_response`), one per SQL query (`app_sql_stats`), a goroutine gauge —
and it does so without a line of instrumentation in this repository. Spans are
the same: every request is traced by the framework's middleware, which is why
tracing works here with no span code anywhere.

What it cannot know is whether the application is doing its job. A deployment can
serve every request in milliseconds while nobody has been able to book time since
the directory changed. So these are recorded here as well, each because
somebody would act on it:

| Metric | Says |
| --- | --- |
| `gtr_timesheet_hours_booked` | hours per entry — the sum is what was recorded, the count in how many pieces |
| `gtr_signin_failures_total` | refused sign-ins, by reason — `credentials` is somebody guessing, `totp` a second factor that did not take, `directory` a directory that stopped answering, `database` this installation's own database that did |
| `gtr_directory_accounts_total` | accounts the synchronisation created or deleted — the one operation that removes people together with their hours |
| `gtr_directory_scheduled_runs_total` | what each scheduled synchronisation came to — `completed`, `refused` by a guard, or `failed`; GoFr's own `app_cron_job_success` counts all three as a success |

None of them carries a user, an address or a project name as a label. A label is
a time series: one per person is both a memory leak in the collector and a list
of who works here, published on a port that asks for no password.

**Before writing an alert:** a metric is published only once it has a value, so
an installation that has had no refused sign-in publishes no
`gtr_signin_failures_total` at all rather than publishing it as zero. Treat an
absent series as absent — `absent()` — rather than as a healthy zero.

Operations: `/.well-known/health` and `/.well-known/alive` on the application's
own port, which is what the image's healthcheck asks — and `/metrics` on port
2121, a port of its own, outside the middleware chain, which therefore asks for
no sign-in, is not covered by TLS, and serves Go's profiling endpoints under
`/debug/pprof/` beside the metrics. Reach that one from your monitoring, not from
the internet, or switch it off under *Settings*.

This sentence used to put all three on 2121. They are not: the two health paths
answer on the application port and 404 on the metrics port, which is why
`wget http://127.0.0.1:8000/.well-known/alive` is what
[`Dockerfile`](../Dockerfile) and [`deploy/compose.yaml`](../deploy/compose.yaml) both
run.

Neither of them means as much as it looks like. `/.well-known/health` answers
`200` even when the SQL datasource is down — the state is in the body, as
`"status": "UP"` or `"DEGRADED"` — so alerting on the status code alerts on
nothing. And `/.well-known/alive` answers `200` while the **installer** is
running, because the installer serves its page for every path, so a container
sitting on the installer with no application behind it reports healthy. The
honest liveness check for "is the application actually up" is
`/api/v1/branding` carrying a `version`, which is what the release workflow's
smoke test asks.

`METRICS_PORT` switches the endpoint off only on exactly `0`. Any other value it
cannot parse — `off`, `false`, `-1`, empty — falls back to 2121 and switches it
**on**, which given what that port serves is worth getting right.
