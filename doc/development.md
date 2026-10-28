# Development

How to run it while changing it, and what a change has to pass before it is
merged.

| Task | What it does |
| --- | --- |
| `task dev` | **Develop.** Backing services, then the locally built binary against them, on :8000 |
| `task dev:watch` | **Develop.** The same, built and started again whenever the code, the interface or the configuration is saved |
| `task test` | Unit and integration tests |
| `task stage` | **Verify.** The shipped container image against real services, on :8080 |
| `task image` | **Ship.** Build the deployment image |
| `task release VERSION=v1.2.0` | **Ship.** Tag a minor or major version by hand; a patch needs no command at all |

`task` on its own lists everything, and `task --summary <name>` explains one.

## The three environments

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

Production configuration is [`deploy/`](../deploy/) — two compose files and an
environment template, and nothing from the source tree. See
[Deployment](deployment.md).

## Trying the installer

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
[`install_test.go`](../test/integration/install_test.go), which drives it through
`harness.StartUnconfigured`.

## Surviving a network that misbehaves

Multi-statement writes are transactional, so a connection lost half way through
leaves the database describing something nobody asked for: see
[`sqldb.base.withTx`](../internal/infrastructure/persistence/sqldb/sqldb.go) and the
commit that introduced it. The directory dials with a bounded timeout, so an
unreachable LDAP server delays one sign-in rather than holding a request open.

GoFr's **circuit breaker** does not apply here, which is worth stating rather than
leaving somebody to look for it. It lives in `pkg/gofr/service` and guards
*outgoing HTTP calls* registered with `AddHTTPService` — it takes a `HealthURL`
and polls it. Nothing here is registered that way, and there is nothing to
register: the work this application does is a SQL database and an LDAP server,
neither of which is HTTP, and the optional trace exporter is gRPC and managed by
the framework.

Two outgoing HTTP paths do exist, and a breaker helps neither. Both live in
[`tlsserver`](../internal/infrastructure/tlsserver/tlsserver.go) and only when the
binary terminates TLS itself: `autocert` talks to Let's Encrypt to obtain and
renew a certificate, and the redirect listener proxies to this same process. The
first already retries on its own schedule and is not on any request's path — a
breaker around it would trip while nobody was waiting. The second is a hop to
localhost, where the thing a breaker protects you from is the thing that would
have tripped it.

So the conclusion stands, but not for the reason this paragraph used to give: it
claimed the process makes no outgoing HTTP calls at all, which was simply untrue,
and an argument that is wrong on the facts is worse than the missing feature it
was defending.

**Caching** is likewise deliberate rather than absent, and there are three kinds
of it here.

*In process*, two things are cached, both because they are read on nearly every
request and both with the staleness written down: the maintenance state for two
seconds ([`maintenance.go`](../internal/interface/api/v1/rest/maintenance.go)) and
the operational limits
([`limits_provider.go`](../internal/application/v1/service/limits_provider.go)), each
invalidated on save so a change takes effect on the next request rather than at
the end of an interval.

*In the browser*, the interface itself is revalidated rather than re-sent. Every
embedded asset carries an ETag computed once at start-up — it cannot change while
the process runs — and `Cache-Control: no-cache`, so a reload asks and is answered
`304 Not Modified` with no body
([`web.go`](../internal/interface/web/web.go)). `no-cache` rather than a long
`max-age`, deliberately: the asset URLs are not fingerprinted, `/app.js` is
`/app.js` in every release, so a browser told to hold one for a year would run the
previous interface against the current API with no way to be told otherwise. This
was missing entirely until it was measured — `http.FileServer` derives its
validators from the modification time, and a file compiled in with `embed` reports
the zero time, so nothing was ever sent and every page load refetched the whole
interface.

*Not at all* for API answers, which is the right answer for them: they depend on
who is asking and several change between two requests a second apart. GoFr can
bring Redis, and for an installation the size this is built for that would mean
one more service to run, back up and keep reachable in exchange for queries that
already answer in under a millisecond against a local file.

## Develop

```bash
task dev                 # PostgreSQL + seeded directory, then the app
task dev DB=sqlite       # no containers at all, straight onto a local file
task dev:watch           # either of those, rebuilt and restarted on every save
task env:down            # stop everything and delete the data
```

The application runs in the foreground; `Ctrl-C` stops it and leaves the
containers up, so the next start is quick. `task dev:watch` puts
[air](https://github.com/air-verse/air) in front of it, at the version the
Taskfile pins and without installing anything: a saved Go file, `app.js`, the
stylesheet, the markup, a dictionary or a `.env` under `cmd/configs` builds the
binary again and starts it, a few seconds later. The page is not reloaded for
you, and a build that fails stops the running process, so the browser cannot
show code that is no longer in the editor. [`.air.toml`](../.air.toml) says what is watched. LDAP is not configured through the
environment — it is administered in the running application under *Settings*;
[`test/README.md`](../test/README.md) lists the values and the seeded accounts
that make the synchronisation's edge cases reproducible.

## Adding a language

Every language but English is one file - German is
[`internal/support/i18n/locales/de.json`](../internal/support/i18n/locales/de.json) -
and everything that says something reads it: the page, the installer, the
terminal client, the workbooks, the calendar feed and the foot of a PDF. English
has no file. It is written where it is said, in the markup and beside every
lookup, so a sentence a dictionary lacks is said in English rather than not at
all.

A new language is two changes:

1. A copy of `de.json` named for the language's code - `fr.json`, say - with
   every sentence translated. `language.name` is what the language calls
   itself, `number.point` and `number.group` how it writes a figure, and
   `layout.day` and `layout.time` how it writes a day and a time, as Go layouts
   written with the reference day: `02.01.2006` writes 27.08.2026, and `15:04`
   writes 23:40. A sentence takes its values where it says `{0}`, `{1}`.
2. The code in `SupportedLanguages`, in
   [`internal/domain/model/user_model.go`](../internal/domain/model/user_model.go).

Nothing else names a language: the choosers, the manifest, the installer and the
terminal client take the list from the files. `task test` says what is missing -
a key the new file lacks, or has and German does not, a file not written the one
way, a figure or a day it cannot write, or a language offered with no file.

## Verify

```bash
task test                # unit tests
task test:integration    # the real binary, driven over HTTP, end to end
task test:browser        # the real interface, driven in a real browser
task test:firefox        # the same interface, in a second engine
task stage               # the real image, against real services, on :8080
task stage:logs          # follow its log
task stage:down          # stop it and delete the data
```

**Integration tests** ([`test/integration/`](../test/integration/)) start the
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

**Browser tests** ([`test/browser/`](../test/browser/)) click through the real
interface with Chrome, Chromium or Edge. They cover what no API test can:
whether the application is *usable*. A sign-in overlay that never goes away, a
tab that highlights but changes nothing, a stylesheet rule that quietly beats
the `hidden` attribute — every one of those leaves the API perfectly healthy
and the application unusable. This project shipped exactly that once.

```bash
task test:browser
```

**Firefox tests** ([`test/firefox/`](../test/firefox/)) run the same application in
a second engine. Chrome and Edge are both Blink, so the suite above covers two
browsers and one renderer — and the difference is not academic: bulk deletion was
invisible in Firefox for as long as it existed, because one CSS rule written for
form fields reached a checkbox in a table cell and made it nought pixels wide.
Every Blink test passed throughout; they ask the document what is there, and the
document was right.

So this suite asks what size things are, and whether the page declares one icon
or two. It speaks WebDriver BiDi over a WebSocket that Firefox opens itself, so
there is no geckodriver to install or keep in step with the browser.

```bash
task test:firefox                      # skips itself where there is no Firefox
FIREFOX_PATH=/path/to/firefox task test:firefox
```

Or all of them at once, in the order that fails fastest — a compile error should
not be found after twenty minutes of browser automation:

```bash
task test:all              # unit, then integration, then browser, then Firefox
task test:all DB=postgres  # with the integration leg against PostgreSQL
```

CI runs all of it on every push and before every release, as eight jobs in
parallel: lint, unit tests with `-race`, integration against **all three
dialects** as three separate legs, the directory and tracing suites against a
real OpenLDAP and a real collector, the browser suite in headless Chrome, a
cross-compile of every published platform, and the Docker image built and
smoke-tested. Nothing here is only exercised locally — `task test:ldap` and
`task test:traces` skip themselves without their containers, so CI is where
they are guaranteed to have run.

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
[`ci.yml`](../.github/workflows/ci.yml) enforces them independently.

```bash
golangci-lint run ./...   # configured in .golangci.yml
```

CI runs the version named in
[`.golangci-lint-version`](../.golangci-lint-version) rather than whatever is
newest, so a push cannot fail on a commit that passed yesterday because a new
release added a check. Keep the locally installed version in step with that
file — `golangci-lint --version` — or local runs and CI will disagree about
clean.

**Which build is this?** The footer names the version and the platform it is
running on — `v1.2.0 (windows)`. The same version is published for four platforms
and they do not all behave alike, so the version alone does not answer the first
question of a support conversation.
