# Working in this repository

A time recording application: one Go binary that serves its own web interface,
its migrations and its timezone database, plus a container image and a
deployment for people who prefer one.

This file is what an agent needs to know before touching it. It is not a
tutorial — `README.md` explains the application, and `deploy/OPERATIONS.md`
explains running it. This is about how work gets done here.

## The one rule that is not negotiable

**Nothing reaches `main` that CI has not passed, and nothing is released that
has not been merged.**

The order is: push a branch → CI runs on the pull request → merge only when
every check is green → CI runs again on `main` → the release is cut from that.
`.github/workflows/release.yml` enforces the second half; the first half is
yours to honour. Check the result before merging — do not chain the check and
the merge into one command, because a failing check does not stop the next
command in a pipeline.

A red check that turns out to be an unrelated flake is still a red check. Re-run
it and merge the green result.

## What a change looks like here

**Every change is proven, not asserted.** A fix arrives with a case that fails
against the unfixed code and passes against the fixed one, and the commit says
so. If a case cannot be made to fail before the fix, it is testing something
else — say that out loud rather than letting it pass for proof.

Reproduce before diagnosing. Several bugs in this repository were explained
twice, wrongly, before somebody measured: the port search, the tab that would
not switch, the sign-in screen that came back. When a wait times out, make the
failure report what the page or the process actually looked like, then read it.

**Comments say why, not what.** The style here is long-form and specific: what
went wrong, what was tried, what it cost, and why the code is the shape it is.
Match the density of the file you are editing. A comment that repeats the line
below it is noise; one that records the failure that produced the line is the
reason anyone can change it later.

Commit messages follow the same idea. A subject line that reads as a sentence,
then prose explaining the problem — not a changelog entry.

## The suites, and when to run them

```
task test              # unit
task test:integration  # against sqlite, postgres and mysql
task test:browser      # Chrome, via chromedp
task test:firefox      # Firefox, via WebDriver BiDi
task test:all          # all of it
golangci-lint run --build-tags "browser integration firefox" ./...
```

Run lint and the unit suite on anything. Run the integration suite when a
handler, a service or a migration moves. Run the browser suite for anything the
interface does — it is the only place that can see a browser-side break, and it
has caught several that read perfectly in the source.

The browser suite starts one application instance per case and runs them in
parallel, so it is sensitive to machine load. A failure that does not reproduce
alone is usually load; say "does not reproduce alone" rather than "flake", and
prefer fixing the missing wait to raising a timeout.

`task clean` removes what an interrupted run leaves behind, including headless
browsers.

## Things that have bitten, and will again

- **`hidden` is an `HTMLElement` property.** SVG elements do not have it —
  `element.hidden = true` there creates a property nobody reads. Use the
  attribute, and give it a `display: none` rule, because a `display` declared
  elsewhere beats a bare `hidden`.
- **`form.id` is not the form's id** when the form holds a control named `id`.
  The control wins. Use `getAttribute('id')`.
- **A `<select>` has no empty option.** Setting an unmatched value shows blank,
  and a browser restoring the form lands on the first entry instead.
- **Two implementations of one feature is the recurring defect here** — two
  password reveals, nearly two brand marks. Before adding a control, search for
  one that already exists.
- **The interface refills itself** after saves, language changes and background
  loads. Anything that fills a form must leave alone a form somebody is part
  way through, and anything that reads a copy of server state must not read one
  the reload behind a notice has not refreshed yet.
- **GoFr reads its configuration in `gofr.New()`.** The metrics port, the trace
  exporter and the database connection are decided before there is a screen, so
  administering them means storing them and reporting a pending restart.

## Layout

```
cmd/            main, and the configuration files compiled into the binary
internal/
  domain/       models and repositories - no framework, no transport
  application/  services: the rules, one layer above the repositories
  infrastructure/ config, persistence, directory, self-update, TLS
  interface/    REST handlers, the embedded web assets, the installer
  pkg/          small self-contained helpers (imaging, qrcode, spreadsheet)
test/           harness, integration, browser, firefox, ldap, probe
deploy/         compose files and the operations manual
```

The web interface is vanilla JavaScript with no build step, embedded with
`go:embed`. It is served under a CSP with `script-src 'self'`, so there are no
inline handlers and no inline styles.

Every error the API returns carries a code from a closed catalogue
(`internal/pkg/apperror`), rendered client-side as `t('err.' + code)`. A test
holds every code to having a German sentence, so a new code needs a translation
in the same change.

## Releases

Every merge to `main` cuts the next patch version. `task release VERSION=vX.Y.Z`
tags a minor or major by hand and the next merge counts on from there.

A release publishes the image for `linux/amd64` and `linux/arm64` and a binary
for four platforms. The image is staged in a registry beside the job, started on
both architectures, and only then copied to the registry people pull from.
