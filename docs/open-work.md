# Open work

One item left, with what an investigation already established so nobody spends an
hour rediscovering it. Delete this file when the list is empty.

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
