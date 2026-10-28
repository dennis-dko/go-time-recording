# Getting started

Installing Go-Time-Recording, what its first start looks like, and what the
first sign-in asks for. Running it on a server is the subject of
[`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md).

## Quick start

```bash
task dev DB=sqlite   # build and start on a local file, nothing else needed
# or directly:
go run ./cmd
```

Then open <http://localhost:8000>. The web interface is served by the same
binary as the API.

`task dev` on its own adds PostgreSQL and a seeded test directory in
containers; `task stage` runs the real deployment image against them. See
[Development](development.md).

**Installer.** With no database configured — no `DB_DIALECT` and no
`configs/datasource.json` — the binary does not start the application. It serves
an installer on the same port and waits, because there is nowhere to put an
account, a project or an hour until a database exists.

The connection is tested before it is accepted, written to
`configs/datasource.json`, and then the application takes over the same port **in
the same process**. No restart: a container that exited to finish installing
itself would look like one that crashed.

It asks for a token, printed to the log when the process starts:

```text
no database is configured, so Time Recording is serving its installer instead
open http://localhost:8000 to choose one - the application will not start until you do
setup token: 782106f2d715eaebba8e1c4b93f0a2d1
```

Until a database exists there is no account to authenticate against, and whoever
completes that screen decides where the installation keeps its data — so the one
thing standing between an exposed port and that decision is a value only somebody
who can already see the process can read. Set `SETUP_TOKEN` to choose it yourself
and drive the screen unattended.

The token guards the decision, and what the environment already said about it.
`GET /install/state` needs no token for the instance name and the build version,
which the page shows before anybody has typed anything. Whatever database prefill
the environment supplied — dialect, name, host, port, user, SSL mode — is in the answer
only for a request that carries the token, and the password is in it for nobody.
An installer left reachable from the internet is still worth closing rather than
merely not answering: it stands there until somebody completes it.

Setting `DB_DIALECT` skips the installer entirely, which is how a container
deployment configures itself; see [Deployment](deployment.md). `task dev` sets it
too, so development never meets this screen.

**Answering the installer is the sign-in.** When the application takes the port,
the page that was the installer hands itself over already signed in as the
built-in administrator, and lands on the first thing that matters: choosing a
password. Nothing has to be typed out of the documentation.

It grants nothing that was not already granted. Whoever answered that screen read
the setup token out of this process's log and decided where every account,
project and hour will be kept — and the account they are handed is, until its
password is changed, reachable by anybody who has read the table below. The
handover is refused on an installation configured without the installer, and
stops working the moment a real password is chosen.

**First sign-in.** An administrator account is created on first start:

| | |
| --- | --- |
| User | `admin@local` |
| Password | `changeme123` |

That account **cannot be deleted**, cannot be moved to a role without
administration rights, and is never authenticated against a directory — so an
installation can never lock itself out, and nobody who controls the directory
can take it over.

Until the initial password is replaced the server refuses the ordinary API,
including issuing API tokens. It does **not** refuse the installation itself:
reaching *Settings* goes through a check that resolves the caller without the
initial-password gate, so the database connection, the directory bind,
telemetry, the process log and the restart are all open while `changeme123`
still stands. Treat that password as full control of the installation until it
is changed, not as a limited foothold.

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
and tracing* and applies at once; and it is held in memory in a
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

## Windows says "unknown publisher"

Running the released `.exe` for the first time shows a blue SmartScreen dialog:
*Windows protected your PC — unknown publisher*, with **Run anyway** hidden
behind *More info*.

This is expected, it is not a warning about this application in particular, and
nothing inside the build can remove it. Windows says it about every executable
nobody has signed with a certificate from an authority it already trusts. It is
not a flag, a manifest, a resource or a setting — the released binaries do carry
version information, an icon and a checksum, and none of that counts. The only
thing that removes it is a signature.

**Right now, for one machine.** Either press *More info* → *Run anyway*, or take
the mark off the file first — Windows attaches it to anything downloaded from the
internet:

```powershell
Unblock-File .\go-time-recording-web_v1.2.3_windows_amd64.exe
```

Check the download against the release's `SHA256SUMS` before doing either. That
is the part that actually tells you the file is the one that was published:

```powershell
Get-FileHash .\go-time-recording-web_v1.2.3_windows_amd64.exe -Algorithm SHA256
```

**Properly, for everybody.** A code signing certificate, which costs money every
year and is issued to a legal identity — a company, or a person with documents.
There is no free path and a self-signed certificate does nothing here, because
the point is the authority vouching for the identity, not the cryptography. An
OV certificate (~€200–400/year) names the publisher but may still warn until it
has been seen enough times; an EV certificate (~€300–600/year) or Microsoft's own
Azure Trusted Signing (~$10/month) removes the dialog from the first download.

This project does not sign its releases, so the dialog is what everybody sees.
Signing would go into the release workflow immediately before the checksums are
written — the signature changes the file, and the in-application update verifies
exactly those checksums.
