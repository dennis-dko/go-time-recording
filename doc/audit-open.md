# What the audits have left open

The audits' leftovers that have to outlive them: decisions put to the
maintainer - the code is right and the taste is arguable, a fix that buys
little, or one of the critical decisions `CLAUDE.md` names - and measurements
that reading cannot make. They used to live in `AUDIT_REPORT.md`, a git-ignored
scratchpad that is now started fresh for every audit, so they live here.

An item is added in the commit of the read that raises it, and removed in the
commit that settles it, with the decision in that commit's message.

Carried over on 2026-10-09 from the report of the audits since late September.
Left out as settled by then: D-new-9 (a restart and a cold start give one answer
since the restart hands on the environment the process started with), D-new-26
(the installer's prefill behind the token, #284), D6 (MySQL refuses to start in
another zone, #317), D1 and D2 (no such comments and no such splits remain).
What is below was raised and is not recorded as decided anywhere.

## Decisions waiting on the maintainer

| | Question |
| --- | --- |
| D-new-2 | The HTTPS front end has no read timeout, because large imports are legitimate. Bound it, from a measured upload size and speed? |
| D-new-3 | GoFr's own server bounds only a request's headers. Behind the HTTPS front end it is loopback-only; without TLS it faces the network. A change in GoFr, a front end on the plain port as well, or keep-alive off? |
| D-new-4 | Guessing a second-factor code is bounded only by the per-address rate limit. A per-account limit on wrong codes, with its lock-out trade-off? |
| D-new-6 | The swap drops the error of putting the running binary back; if that fails as well, the path is empty and the message does not say so. Needs a seam to test. |
| D-new-8 | A stop arriving within the restart's half-second grace, with a shutdown lasting past it, lets the re-execution replace the process mid-shutdown until the service manager kills it. A fix that buys little: it needs a seam only a test would use. |
| D-new-10 | A person whose directory entry lost its mail keeps the account through a run but cannot sign in, and the refusal tells them to change the attribute setting. Allow the sign-in keyed on the identifier? |
| D-new-11 | The project list loads every project and filters in memory: 11.8 ms a call on SQLite over 10,000 projects, against 23 µs for the indexed query. A decision about scale. |
| D-new-12 | TLS is configured but HTTPS cannot start: the process carries on in plain HTTP and says so at ERROR. Refuse to start instead - failing closed, with an outage? |
| D-new-13 | 39 of 50 write operations in the OpenAPI document have no request body schema. |
| D-new-14 | The automatic Kerberos attempt on opening the sign-in screen shows the browser's credential dialog once per tab on a Windows machine that does not trust the address. |
| D-new-17 | Should the installer refuse, before saving, a database holding tables this application did not create? A restored database of this application must still pass. |
| D-new-18 | Hand the log level to GoFr's `ChangeLevel`, now that it is atomic? It removes the forced DEBUG and the restart card's one exception, with a wide diff. |
| D-new-19 | The log viewer shows a trace only as a tooltip, which cannot be copied. Click a line to search for its request? |
| D-new-21 | A failed handler's line is raw JSON. Show it summarised as its error? |
| D-new-22 | When capturing the output fails, the log handler still gets the sink - available and empty for ever - and its "not installed" sentence is unreachable. Hand it nothing then? |
| D-new-25 | A record of deletions that no log level can switch off? |
| D-new-27 | In a container a restart is an exit, because re-executing used to inherit the environment; that reason is gone. Replace the process there too? |
| D-new-31 | Make an empty PostgreSQL password work by quoting what GoFr receives? It moves installations whose data sits in the account's own database. |
| D-new-32 | Refuse to start, rather than warn, on a connection the driver reads differently from how it was typed? |
| D-new-33 | Report the unquoted connection string to GoFr upstream? |
| D-new-36 | A directory sign-in under way while the identifier attribute is changed can record an identifier read under the old attribute, and its owner is refused from then on, until the attribute is changed again. Closing the window means the settings service applying the directory configuration itself and forgetting the identifiers a second time after it - worth it, for milliseconds around a change made once? |
| D-new-37 | The recover() in `spreadsheet.rowsOf` has no known panic left to catch: go.mod requires the upstream excelize commit that fixes GHSA-wcg2-648h-mhxq and the fifteen advisories of 2026-10-09. Keep it for the next one, given that record, or remove it as the test that watched it intended - a recover() with no reason left? Either way, go.mod moves from the pseudo-version to the first release that carries the fixes. |

## Measurements reading cannot make

Each needs real infrastructure, and none is a change to make on a guess.

- Whether an account can come to depend on a directory entry behind a referral.
  A run collects referrals and does not follow them; `task test:ldap` runs
  OpenLDAP, and this needs a real Active Directory.
- Whether a desktop browser's history keeps the `/?token=` address the
  installer's page was opened with. The page takes it out of the address bar at
  once; headless Chrome records no history, so this needs a real browser.
- The wall time of a first directory run over thousands of accounts on SQLite,
  and on the Pi: one commit per arrival.
