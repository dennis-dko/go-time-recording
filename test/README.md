# Test environment

Throwaway backing services for trying the application against something other
than SQLite, and against a real directory — plus a probe that checks a
connection using the application's own drivers.

Everything here is **disposable and deliberately insecure**: fixed trivial
passwords, no volumes, no restart policy. It is not a deployment example; the
`Dockerfile` in the repository root is.

## Start what you need

Profiles keep it to the service under test — starting three databases to
exercise one is a slow way to discover the third does not fit in memory.

```bash
cd test

docker compose --profile postgres up -d      # PostgreSQL on localhost:55432
docker compose --profile mysql    up -d      # MySQL      on localhost:53306
docker compose --profile ldap     up -d      # OpenLDAP   on localhost:5389, browser on :5080
docker compose --profile traces   up -d      # Jaeger     on localhost:54317, browser on :51686
docker compose --profile all      up -d      # everything
docker compose --profile stage    up -d      # the shipped image, and the three
                                             # services it runs against

docker compose --profile all down -v         # and clean up
```

The published ports are deliberately unusual, so a running instance of any of
these on the default port is left alone.

## Check a connection

The probe is a small Go program that opens the connection with the **same
drivers the application uses** and runs the same kind of query. That is the
point: a port check or a `psql` session proves a socket answers, not that this
binary's driver can authenticate with these settings.

```bash
# From the host
go run ./test/probe \
  --db postgres \
  --dsn "postgres://gtr:gtr-test-password@localhost:55432/go-time-recording?sslmode=disable"

go run ./test/probe \
  --ldap ldap://localhost:5389 \
  --base-dn dc=example,dc=com \
  --bind-dn cn=admin,dc=example,dc=com \
  --bind-password gtr-test-password

# From inside the network, where the service names resolve
docker compose --profile probe run --rm probe
```

A check that passes from the host but fails from inside the network is a name
or port problem, not a credentials problem — running it both ways is how that
gets distinguished in one step.

### What the LDAP probe actually checks

Beyond bind and search it reports, per entry, whether the directory hands out
the **stable identifier** the synchronisation matches accounts on. This is the
check worth running before pointing the application at a real directory:

- **every entry has it** — a rename cannot be misread as a departure.
- **some entries have it** — the rest fall back to matching on the mail
  address, and a rename of those would look like the person left.
- **none have it** — the probe fails. Everything would be matched by mail
  address, so renaming a mailbox would delete the account together with every
  hour recorded against it.

OpenLDAP and most others assign `entryUUID`. Active Directory uses
`objectGUID`, which is binary and is shown hex-encoded:

```bash
go run ./test/probe --ldap ldaps://dc.corp.example \
  --id-attribute objectGUID --user-filter '(sAMAccountName=%s)' ...
```

## Point the application at these services

Usually there is nothing to do: `task dev` starts these services and connects
the application to them in one step, and `task stage` does the same with the
real deployment image. Both are described in the [main README](../README.md#development).

The settings, for running it some other way:

```bash
DB_DIALECT=postgres DB_HOST=localhost DB_PORT=55432 \
DB_NAME=go-time-recording DB_USER=gtr DB_PASSWORD=gtr-test-password \
DB_SSL_MODE=disable ./go-time-recording
```

The directory is configured in the running application under **Settings →
LDAP**, not through the environment, so:

| Field | Value |
| --- | --- |
| Host / Port | `localhost` / `5389` |
| StartTLS / LDAPS | both off (this test server is plain) |
| Bind DN | `cn=admin,dc=example,dc=com` |
| Bind password | `gtr-test-password` |
| Base DN | `dc=example,dc=com` |
| User filter | `(\|(uid=%s)(mail=%s))` |
| Name / Mail attribute | `cn` / `mail` |
| Unique ID attribute | `entryUUID` |

Then sign in as `alice` / `alice-password`.

Tracing is configured in the running application too, under **Settings →
Metrics and tracing**, and applied at the **next start** — GoFr builds the
exporter inside `gofr.New()`, so nothing saved there can reach the process that
saved it:

| Field | Value |
| --- | --- |
| Trace exporter | `OTLP` (or `Jaeger` — the same code path in this GoFr version) |
| Collector | `localhost:54317`, with **no** `http://` in front of it |
| Share of traces recorded | `1` |

Restart, make a request, and the trace appears at http://localhost:51686 under
this instance's `APP_NAME`. Spans are batched, so give it a few seconds.

## Run the shipped image against all of it

```bash
task stage
```

Builds the deployment image from the repository `Dockerfile` and starts it
against everything it can be exercised against at once - PostgreSQL, the seeded
directory, and a collector its spans have to arrive in - so "does the real
artifact work with all of it" is one command rather than a set of profiles put
together by hand, with the one that was left out found out about later.

Before it reports the environment as up it runs the probe from inside the
network: healthy containers and a reachable network are different statements,
and only the second one is worth starting to click through.

| What | Where |
| --- | --- |
| Application | <http://localhost:8080>, as `admin@local` / `changeme123` |
| Metrics | <http://localhost:8021/metrics> |
| Traces | <http://localhost:51686> |
| Directory browser | <http://localhost:5080> |

Tracing is configured through the environment there, so spans arrive from the
first request. The directory is not, and cannot be - LDAP is administered in the
running application under Settings, so until the values above are entered the
image is running beside a directory it has never contacted. Entering them, and
then signing in as `alice`, is the point of a staging run.

With one change to those values: the image is inside the compose network, where
the service name resolves and the published port does not exist. Host `openldap`
and port `389`, not `localhost` and `5389` - the same distinction the probe is
built to tell apart.

`task stage:logs` follows the application log and `task stage:down` stops it and
deletes the data. Nothing here survives that, by design.

## Check that a span actually arrives

```bash
task test:traces
```

Starts Jaeger, configures an instance through its own settings API, restarts it,
makes a request and reads the trace back out of the collector. It is the only
check here that shows a span arriving anywhere: every other way tracing fails is
silent — an exporter GoFr does not recognise drops each batch after logging
once, a collector address it cannot dial fails inside the exporter, and a
sampler that records nothing looks exactly like a collector with nothing to
show.

Without `GTR_TEST_JAEGER` set the cases skip rather than fail, so
`task test:integration` on its own is unaffected.

## Check that the directory really works

```bash
task test:ldap
```

`internal/infrastructure/directory` is the one package here that talks to
something this project does not control, and the synchronisation tests drive a
fake — which proves the rules and nothing about the LDAP: not the bind, not the
filter substitution, not the attribute names.

The case that matters most is the one the seed was built for. An entry that can
sign in but does **not** appear in the directory listing produces a local account
the next synchronisation reads as a departure, and deletes together with every
hour recorded against it. A fake cannot show that, because it returns whatever
the test told it to. A real directory did, the first time it was asked.

Both suites are part of `task test:all`, which therefore needs Docker.
`task test` on its own stays containerless.

`task test:all DB=postgres` runs the integration suite twice, against the server
and then against SQLite. The second pass is not redundant - a handful of cases
start two instances against one shared database file, and the harness gives each
instance a database of its own on a server, so on a server they skip themselves
and the run reports success having never checked that what an administrator
saves is applied by the next start. The run tidies up after itself when it
finishes, however it finished; a single failing suite is re-run on its own, and
`task test:ldap` and `task test:traces` each leave their container standing for
looking at.

## The seeded accounts, and why each one is there

Each entry in [`ldap/01-seed.ldif`](ldap/01-seed.ldif) stands for a case the
synchronisation has to get right:

| Entry | Password | Stands for |
| --- | --- | --- |
| `alice` | `alice-password` | an ordinary account; the baseline |
| `bob` | `bob-password` | the rename case (below) |
| `carol` | `carol-password` | no mail address; must be skipped, not guessed at |
| `dave` | `dave-password` | outside `ou=people`; appears or not depending on the base DN |
| `service` | `gtr-test-password` | the bind account — not a person, holds no hours |

### Reproducing the rename case

This is the failure the stable identifier exists to prevent. Sign in as `bob`
once so the account exists locally, book an hour, then rename his mailbox:

```bash
# -T matters: without it compose allocates a TTY and swallows the heredoc.
docker compose --profile ldap exec -T openldap ldapmodify -x -H ldap://127.0.0.1 \
  -D 'cn=admin,dc=example,dc=com' -w gtr-test-password <<'EOF'
dn: uid=bob,ou=people,dc=example,dc=com
changetype: modify
replace: mail
mail: robert.builder@example.com
EOF
```

Now run **Settings → Directory sync → Preview**. Bob must *not* be listed for
deletion: he is matched on `entryUUID`, which did not change. Matching on the
mail address — what the application did before — would have listed him, and a
real run would have deleted him along with the hour he booked.

To check the other direction, delete `dave` from the directory entirely and
preview again: he *should* be listed, because he really is gone.

```bash
docker compose --profile ldap exec -T openldap ldapdelete -x -H ldap://127.0.0.1 \
  -D 'cn=admin,dc=example,dc=com' -w gtr-test-password \
  'uid=dave,ou=contractors,dc=example,dc=com'
```

## Why the directory is built rather than pulled

[`ldap/Dockerfile`](ldap/Dockerfile) builds a small OpenLDAP on Debian instead
of using a stock image. Bitnami withdrew their OpenLDAP images from Docker Hub
in 2025 and the popular osixia image is archived upstream, so a test
environment depending on either is one deprecation away from not starting —
which is how this file came to be written in the first place.

The whole configuration is the three files next to it: [`slapd.conf`](ldap/slapd.conf)
in the readable single-file form, [`entrypoint.sh`](ldap/entrypoint.sh) which
seeds offline with `slapadd` before opening the port, and the seed itself. The
first build takes a minute; after that it is cached.

## Browsing the directory

phpLDAPadmin runs on <http://localhost:5080> — log in as
`cn=admin,dc=example,dc=com` with `gtr-test-password`. Worth reaching for when
a bind fails and the reason needs looking at rather than guessing at.
