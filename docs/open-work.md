# Open work

Two items left, with what an investigation already established so nobody spends
an hour rediscovering it. Delete this file when the list is empty.

Everything else that was here has been built, and the decisions that were recorded
here now live where they are acted on: the versioning scheme is in
`.github/workflows/release.yml` and the Deployment section of the README, and the
licence is corrected. `git log` is the record of the rest, which is the only place
it survives.

## Excel export and import of time entries

Export is the easy half. Import is where the care goes: it writes many rows from a
file somebody assembled by hand, which means a partial failure has to roll back
(the transaction seam is `sqldb.base.withTx`), and every row needs validating
against the same rules the API enforces rather than a copy of them.

That last point is now sharper than when it was written. `validateTimesheet` also
bounds the description and refuses a status other than open on creation, and
`model.TooLong` bounds the text columns the servers enforce and SQLite does not —
an import that went around any of it would store rows the API would have refused.

## Charts of your own time, and a clickable calendar entry

Bar or column charts over your own hours. No external chart library: the strict
`Content-Security-Policy` blocks every external origin, and the assets are
embedded, so it has to be hand-drawn SVG or canvas. `createElementNS` rather than
`createElement`, as the password reveal icon and the installer's do — an `<svg>`
built in the HTML namespace parses without complaint and renders nothing.

One thing to settle first: `reports:read` is deliberately withheld from both
default roles, so the existing project report is the built-in administrator's
alone. Statistics over *your own* time therefore cannot go through it, and want
their own endpoint keyed on the caller rather than on a project id — which is also
what "a report for all projects, or for entries with no project" needs.
