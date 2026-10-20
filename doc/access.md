# Accounts, roles and sign-in

Who may do what, the ways somebody signs in — a password, a passkey, a
directory, an organisation's provider — and how a script reaches the API.

## Access control

Users sign in with their email address and password. What someone may do comes
from their **role**, and roles are administered at run time through the
interface: create them, set their permissions, delete them.

| Role | Purpose |
| --- | --- |
| `admin` | The installation, its accounts and their roles — and nothing else. A system role: it cannot be deleted or stripped of permissions |
| `user` | Keeps their own time, projects and calendar |
| `user-admin` | Both: somebody who works here and also administers |

The third one is the answer to "somebody here needs to administer as well". It is
handed out by the built-in administrator, so holding both jobs is a decision somebody
made rather than something an account arrived with — and it is an ordinary account
gaining administration, never the built-in account gaining a working day.

The everyday role was called `employee`, and its combined one `employee-admin`. The word
said more than this application knows: it holds accounts, and whether the person behind
one is employed here, contracted, a volunteer or the only person in the company is not
something it records, checks or needs. Renamed on upgrade, in the database as well as in
the code — every account points at its role by id, so nobody is moved and nobody loses
anything, and the role name stored in the directory configuration is brought along. An
installation that already had a role of its own called `user` keeps it, moved aside as
`user-2` with its rights and its people intact: the shipped name has to win because the
application looks this role up by it, and merging two roles would change who may do what.

What you see is not the identifier. Those names are lowercase, hyphenated and English
because that is what the API takes, what the directory configuration stores and what the
role editor edits — and none of that is something to put in front of somebody deciding
what a colleague may do. Each role is shown by a translated title with its description
beside it, so a German reader chooses *Benutzer & Administrator* rather than
`user-admin`. The screen that administers roles keeps the identifier visible next to the
title, because that screen is where it matters. A role an installation named itself
shows whatever was typed, which is the only sensible answer for words this application
has never seen.

Permissions are fine grained — reading separately from writing, projects separately
from time — and every one of them is scoped to the person holding it. A filter naming
somebody else is refused rather than quietly answered with your own rows.

There is no wider variant. `timesheets:read:all` and `timesheets:write:all` used to
exist beside the `:own` ones, and they were the last of the manager: one opened
everybody's entries, balances, totals and exports, the other let an account book time
in a colleague's name. No role held either by the end, and the four screens that asked
"which person" had been narrowed to a dropdown with a single entry — so ticking one in
the role editor changed nothing anybody could see, while the API answered every
question about every colleague. A capability with no screen is a capability nobody
audits, so both are gone.

Permissions are Go constants, not database rows: each one is checked by a
specific line of code, and a permission that existed only in the database would
grant nothing. The role editor therefore offers exactly the permissions that
are actually enforced.

One of them is the whole administration: `settings:manage` opens the *Settings*
screen and everything on it — the branding, the operational limits, the instance
timezone, the database connection, the telemetry, the maintenance notice, the
log and the restart. It is what the `user-admin` role holds that `user` does
not, and it is why that role is handed out deliberately rather than assembled
out of smaller rights. Two things stay outside it and belong to an account that
administers and has no working day of its own - the built-in administrator, or
one holding the `admin` role: the setup wizard, and running or scheduling a
directory synchronisation. On the *Settings* screen of a `user-admin` account the
synchronisation card is replaced by a sentence saying so.

### The administrator does not work here

Running an installation and recording time in it are two different jobs, and the
`admin` role does only the first: accounts, roles, the database, the directory, the
log. It has **no** working day at all — it cannot book an hour, keep a project, read
a figure or set a daily target, its own included.

Backups used to be in that list and are not a thing this application does: there is
no permission for them, no endpoint and no screen. They are external, and
[`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md) says what to copy for each dialect.

That is deliberate, and the reason is the account itself. Every installation has the
built-in administrator before anybody has chosen anything: it is how you get in, not
somebody's working day. Whoever does work here has an account of their own, and if
that person also administers, they are given the `user-admin` role rather than
made to sign in twice.

It used to book and read its own hours "like anybody who works here", and that was
the wrong shape — an account nobody chose, quietly holding a working day nobody asked
it to have.

What it does own is the installation's daily ceiling under *Settings*, which bounds
every account's day. A new account's target is the built-in eight hours; each person
changes their own from there, and nobody changes anybody else's.

On upgrade, the working day is taken off the `admin` role and the combined role is
created first, so an installation that had been using the built-in account for
both jobs has somewhere to move that person. Nobody is moved automatically: who works
here is not something a database can work out, and guessing would either invent a
colleague or take an administrator's hours away. The entries stay in the tables and
become reachable again the moment the combined role is assigned.

It is a wall rather than a default. The `admin` role's permissions cannot be changed
at all — neither given nor taken — and the role editor shows them as unavailable rather
than letting them be ticked and refusing the save.

It was the other way round: whoever may manage roles could widen the role they hold, on
the reasoning that somebody who administers roles can reach anything anyway. That
reasoning does not survive this arrangement. The built-in administrator configures the
installation and records no time, so a right added to its role would hand a working day
to the one account nobody chose — quietly, from the screen that administers roles.

The way past the wall is a decision about a colleague: give a person the `user-admin`
role. Note what that does *not* buy them — it is a user's own rights plus the
administration, so they keep their own hours and still nobody else's.

### Reports

A project's report totals **your own** hours on it. Everyone sees their own figures
there and through **My statistics**, and nobody sees anybody else's.

**Every screen that evaluates a period arrives with one filled in** — the month
so far — rather than waiting to be told. The alternative was worse in both
directions: the hours card used to fill its fields *when Evaluate was pressed*,
so the answer arrived for a period nobody had seen until the fields changed under
it, and the report and overtime forms sent nothing at all and quietly evaluated
the whole history. Refusing to evaluate until a date is typed would be worse
still — there is an obvious right answer here, and demanding it be typed is
make-work. Clear a field and it stays clear; the period is yours once you have
touched it.

The picture beside the table is the same figures in whichever shape answers the
question: **bars** for a long list of names, because a name fits beside a bar and
not under a column; **columns** for comparing a few things; a **pie** for a share of
a whole. **Each project keeps its own colour** across all three shapes and
between visits, because the colour comes from the project rather than from its
place in the list — a colour that means something different on every visit is
decoration rather than information. It follows the filter the table follows, so scoping the evaluation to one
project scopes both — a chart totalling everything next to a table totalling one
project is two answers on one screen, and no way to tell which was asked for.

**Everything you press on a small screen is at least 44px tall** — the size a
finger needs, and the number both platform guidelines settled on. Measured on a
390px screen before this was set: the button that dismisses a notice was 18px,
a permission switch 22, the one that reveals a password 26, the fields 38.
Height only; nothing is enlarged to read, there is simply more of it to hit.

**The navigation folds into one control below 900px.** Nine points do not fit
beside anything on a telephone: as a row that scrolled sideways they showed
three and hid six, with nothing on screen to say the others were there — and
because the bar was as wide as the row, the *window* scrolled in its place, so
the page could be dragged sideways. Measured at 50px too wide on a 390px screen
and 80px on a 360px one, on every screen in the application. Behind the burger
they are a list, one per line, which says how many there are. It closes on
choosing a point, on Escape, and on a press anywhere else.

**The greeting shows this week** to somebody who books time: today's hours
against today's target with a running clock counted in, the week's total, and a
bar per day drawn against that day's own target - the days come from
`/me/days`, which answers every day of a range, the empty ones included.

**The greeting shows the last few entries** to somebody who books time — a
bounded ninety-day window rather than a whole history, because a greeting has no
business downloading three years of a working life to show five lines of it. The
account that records no time is shown no panel at all rather than an empty one.

**Appearance belongs to the person, not to the machine.** Light or dark is
stored on the account, like the language, and applied when that account signs
in. Signed out — and on a fresh account that has never chosen — the screen
follows the time of day. It used to live in the browser, which is right for one
person with one laptop and wrong everywhere else: the next person at a shared
machine arrived to the last one's dark mode.

Everything else this interface keeps in a browser is keyed by account id and so
cannot reach anybody else: the screen each account was last on, whether it has
been greeted, whether the browser's zone and language have been offered, and
which release it has dismissed. The two exceptions are impersonal — the
instance's own name and mark, cached so the first paint is not generic, and a
scroll position kept for the length of one reload.

**The three roles the application ships with are shown, not edited** — not their
name, not their description, not their rights. They are the answer to questions
asked elsewhere: the role every new account gets, the one the directory
synchronisation assigns, and the pair the interface names in its own words. A
role that should grant something different is a new role, which is a minute's
work and says what it is.

**The release feed is asked at most four times a day**, however many
administrators are looking at the screen. It used to be asked once a minute at
most, which sounds careful and is not: GitHub allows an unauthenticated caller
sixty requests an hour *per address*, every administrator's sign-in starts a
check, and every open tab repeats it hourly — so an installation with a few
administrators spent its allowance and got a 403 that reads as a permission
problem with somebody else's service. A failed lookup no longer erases the last
good answer either: the card shows the version it knows, with the trouble noted
beside it. An installation that sets `UPDATE_TOKEN` gets five thousand an hour
and never comes near either number.

**The rights are listed in words**, grouped by area, with the identifier beside
each — the identifier is what the API takes and what a directory configuration
stores, so it stays readable — and a legend under them saying what each one
actually allows.

Whether somebody may see another person's recorded time is not a permission question,
because no permission answers yes — not for a list of entries, not for the spreadsheet
export, not for a project's total and not for an overtime balance. Whose entry it is
decides, and that is not a choice anybody can be granted.

Two rights used to answer it, which is how two answers come to disagree. `reports:read`
covered the totals and belonged to the role that reviewed other people's hours; when
that role went, the right stayed, held by nobody and gating a whole screen, so the
report was unreachable on every installation while appearing to be somebody else's
business. `timesheets:read:all` covered everything else and outlived it by one release.
Both are withdrawn on upgrade, and a role that held one keeps the half of it that is
still a right — whoever could read everybody's time can read their own.

## API access with personal tokens

Every user can issue their own tokens under **My account**:

```bash
curl -H "Authorization: Bearer gtr_xxxxx" https://your-host/api/v1/timesheets
# or
curl -H "X-API-Token: gtr_xxxxx" https://your-host/api/v1/timesheets
```

A token carries **no rights of its own**. Every request is evaluated against
the owner's role *at that moment*, so changing or revoking a role takes effect
on the very next call, and a token can never outrank the person it belongs to.

Only a hash of the token is stored; the value itself is shown once, at
creation, and cannot be recovered afterwards. Tokens can be given an expiry and
can be revoked at any time.

Interactive documentation is at **`/api-docs`**, and the OpenAPI specification
it renders is at `/openapi.json` — load that into Swagger UI, Postman or
Insomnia if you prefer those.

## LDAP

The administrator configures a directory under **Settings**. When it is
enabled, passwords are checked against the directory by binding as the user.

This application is a directory *client*. It does not run one and does not need
one: an installation with an Active Directory or an OpenLDAP points at what it
already has, and an installation with neither carries on with its own accounts,
passwords, passkeys and two-factor codes. For the two cases where neither is
true — trying the feature out, or genuinely wanting a directory and having none
— [`deploy/compose.ldap.yaml`](../deploy/compose.ldap.yaml) runs one beside the
application, and its header says which fields to type where.

Accounts are also created on first successful sign-in, so someone can start
working without being provisioned first. Local-only accounts keep working
alongside directory ones. Roles and permissions always stay local — the
directory decides *who you are*, this application decides *what you may do*.

An account created here under the same address — by an administrator, or by an
import — is taken over the first time its owner signs in through the directory,
hours and all. From then on it is a directory account: it keeps no local
password, so it is no longer asked for a new one and the initial password no
longer opens it, and a synchronisation treats it like any other. An account that
administers this installation is never taken over.

Nor is one that already belongs to another directory entry. A directory account
remembers its entry by the identifier the card names — `entryUUID`, or
`objectGUID` in Active Directory — so somebody given an address another entry
had before is not handed that entry's account and its hours: the sign-in is
refused as vaguely as a wrong password, and the log says at WARN which entry the
account belongs to. It stays the predecessor's until a synchronisation removes
it, which happens once their entry has left the directory; the next sign-in then
starts an account of the newcomer's own. Changing the identifier attribute on the
card forgets the identifiers recorded under the old one, and each account is
matched by its address again until its owner next signs in.

### Signing in with a Kerberos ticket

Where people sign in to a Windows domain, their browser can present its Kerberos
ticket instead of a password. The operator places a keytab on the server and
names it in `KERBEROS_KEYTAB`; the ticket's owner is then looked up in the
directory configured here, and signed in to the same account the directory's
password reaches — second factor included. The sign-in screen tries a ticket by
itself and falls back to the form without one.
[`deploy/OPERATIONS.md`](../deploy/OPERATIONS.md) says how to make the keytab and
what the browsers need.

### Synchronisation

Under **Settings → Directory synchronisation** the whole directory is
reconciled with the local accounts:

- Accounts the directory has and this installation does not are **created**.
- Accounts the directory no longer holds are **deleted**, together with their
  time entries, projects, API tokens and sessions.

**The directory is only ever read.** Nothing is written back to LDAP.

**A run is the built-in administrator's alone** — the preview, the button and
the schedule beside them. Not because of the connection: anybody holding
`settings:manage` may edit the directory card, test it and save it, because
that is configuration. Deleting every account the directory no longer holds,
along with everything those people recorded, is not, and the card is not shown
to an account that would be refused it.

The schedule is part of that. It was open to anybody who could reach *Settings*
while the buttons above it were not, so the caution the buttons were given
could be walked around by typing five numbers into the field between them.

Set the schedule under *Settings*, or `LDAP_SYNC_SCHEDULE` for a starting value
in the environment; it is empty by default because a run destroys recorded work
irreversibly, and an automatic one destroys it with nobody looking. It runs on
the server's clock - UTC in the shipped container, which sets no `TZ` - and not
in the instance timezone, so `0 4 * * *` is four in the morning in Greenwich. Use
**Preview** first, which reports exactly which accounts would go and how many
time entries each one would take with it. The real run asks for confirmation
naming those numbers.

Four guards make a broken directory answer non-destructive:

| Guard | Reason |
| --- | --- |
| A failed directory read aborts with an error | An outage must never be read as "everybody left" |
| An empty result deletes nothing | Almost always a wrong base DN or filter, not a mass departure |
| `LDAP_SYNC_MAX_DELETE_RATIO` (default 0.5) | A truncated or misfiltered answer would otherwise look like half the company leaving |
| Local and built-in accounts are never touched | They were never in the directory, so its silence says nothing about them |

Listing is paged, because directories commonly cap a plain search at 500 or
1000 entries — a silent truncation would read as "everyone beyond the first
page has left".

Every run that removes or adds an account is listed under **Recent runs** on
the card: when it ran, whether somebody confirmed it against a preview, and how
many accounts and time entries it took or brought. It names nobody — the people
a run removed are the people the deletion erased — and the log, which does name
them, can be switched off by its level; this list cannot.

## Signing in through your organisation's provider

Under **Settings**, *Sign-in provider (OpenID Connect)* points the installation
at a provider - Keycloak, Authentik, Entra ID, Google - by its issuer address,
the client ID and secret it registered this installation as, and the name the
sign-in form calls it by. The card shows the address to register at the
provider for sending people back: this installation, followed by
`/sign-in/openid`. *Test the provider* reads its discovery document before
anybody depends on it.

**Nobody is signed in by an address.** An identity at the provider opens an
account only once that account's owner has linked it, under **My account**,
while signed in - with their password and its second factor. No account is
matched on the address a provider reports, and none is ever made for somebody
the provider knows: an installation whose accounts its administrator makes
should not have them made by whoever can sign in at Google. From then on the
provider's sign-in is theirs to use instead of the password, with the
provider's own second factor; the password keeps working beside it, and
*Unlink* takes the link away.

What holds the round trip together:

- **The browser that left is the one that comes back.** The round's state is
  kept in a cookie only this installation can set, so a code the provider sent
  one browser back with cannot sign another one in - which would otherwise let
  somebody have another person's browser signed in as them.
- **Only what the provider signed is believed.** The code is traded with the PKCE
  verifier the round was begun with, and the identity token counts only when it
  carries the provider's published signature, this installation's client, a
  time that has not passed and the round's own nonce.
- **The built-in administrator and directory accounts stay on their passwords**,
  for the reasons a passkey is refused them: the first is the guaranteed way
  back in, and the second has to go through the directory, which is where an
  organisation ends somebody's access.
- **A provider is reached over HTTPS**, unless it runs on this machine: over plain
  HTTP anybody on the way could answer for it and sign anybody in as anybody.
  The client secret is sealed with `SECRET_KEY` like the directory's bind
  password.
