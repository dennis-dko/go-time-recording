# Open work

The remaining items from one batch of requests, with the decisions already taken
so they do not have to be taken twice. Delete this file when the list is empty.

Each item is one pull request. They are sized that way on purpose: several touch
`cmd/main.go` and the settings handler, so two in flight at once means conflicts
and a review nobody can follow.

## Decisions already made

**Versioning.** A merge to `main` releases with the patch number incremented —
CI reads the newest tag and counts on. A tag set by hand is respected, and the
next merge continues from it. There are no tags yet, so the first automatic
release has to start somewhere: `v0.1.0` unless told otherwise.

**Licence.** Stays MIT. Only the copyright holder and year are corrected.

**Order.** Data safety first, then the rest. The reason is not tidiness: an Excel
import without transactions can leave half a month of imported hours behind after
a failure, so the transaction work had to come first. It has.

## Done

- `task clean` removes what testing leaves behind, including processes still
  holding a database file open
- Multi-statement writes are transactional, and three bugs in deleting an account
  are fixed — see the commit, which explains what each one did
- Maintenance mode
- Metrics and traces in the settings. Stored, and applied at the next start by a
  read that happens before `gofr.New()`. The metrics **port** stayed in the file
  on purpose, which the investigation had not foreseen: GoFr calls `Fatalf` when
  something already holds it, so a port saved from the screen could stop the next
  start and take that screen with it. Only switching the endpoint off is
  administered, because that cannot fail.

## Remaining

### Excel export and import of time entries

Export is the easy half. Import is where the care goes: it writes many rows from
a file somebody assembled by hand, which means a partial failure has to roll back
(the transaction seam is in `sqldb.base.withTx`), and every row needs validating
against the same rules the API enforces rather than a copy of them.

### Charts of your own time, and a clickable calendar entry

Bar or column charts over your own hours. No external chart library: the strict
`Content-Security-Policy` blocks every external origin, and the assets are
embedded, so it has to be hand-drawn SVG or canvas.

### Verify every translation, the setup wizard and the guided tour

A check rather than a feature. The German dictionary is keyed by hand and English
is the source, so a key used in the interface but absent from the dictionary
falls back silently and is invisible until somebody switches language. Worth a
test that walks every `data-i18n` attribute and every `t(...)` call and reports
keys with no translation.

### README: how to set up dev, test and prod

`task dev`, `task test:all`, `task stage` and `deploy/` all exist and are
documented in pieces. What is missing is one page that walks the three
environments end to end.

### MIT licence: holder and year

### Release on merge to main

See the decision above. Currently a release is cut only from a tag matching
`v[0-9]+.[0-9]+.[0-9]+`, by `release.yml`; a merge to `main` publishes `:edge`
and `:sha-<commit>` and nothing else.

Two things to get right: the smoke test in `release.yml` needs a database
configured, because the image serves its installer without one, and it has to
check the response body rather than the status - the installer answers 200 on
every path.

## Where the reasoning lives

Not in a chat log. In the commit messages, the pull request descriptions and the
comments next to the code that needed them. `git log` is the record of why, which
is the only place it survives.
