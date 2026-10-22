# Features

What the application does, and the rules it keeps while doing it.

## What is included

| Area | Implementation |
| --- | --- |
| Storage | SQLite (pure Go, no cgo), PostgreSQL or MySQL - chosen in the installer, never defaulted |
| Concurrency | SQLite runs in write-ahead logging, so a save and a page load do not refuse each other |
| Schema | GoFr migrations, applied automatically at start-up |
| API | REST under `/api/v1`, documented at `/api-docs` |
| Web interface | Embedded via `go:embed`, vanilla JS with no build step |
| Access control | RBAC with roles administered at run time; bcrypt password hashes |
| Sign-in | Session cookies, optional passkeys and TOTP two-factor per user, a browser's Kerberos ticket where a keytab and a directory are configured, and an OpenID Connect provider - Keycloak, Authentik, Entra ID, Google - for the accounts their owners linked to it |
| Live log | The process log, filterable and searchable, for the built-in administrator |
| Administration log | Who created, changed or deleted an account or a role, saved a setting, switched maintenance mode or restarted the application, kept across restarts and readable under *Settings* by whoever administers |
| Version | The running build in the footer of every page |
| API access | Personal tokens, scoped by the owner's current role |
| Stopwatch | Start and stop the clock; the measured time becomes an entry, to the second. A start pressed late can be corrected, a clock left running is booked up to the time the work ended, and switching to the next project books the clock and carries on from the same second |
| Reminders | A banner names the last working day with nothing booked on it and leads to the booking form on that day; another says when the stopwatch has run longer than its owner chose - ten hours unless they chose otherwise - with a notification as well where the browser allows one. Both are chosen under *My account* |
| Finding entries | The list narrows by project, by period and by text in the description, and the export follows the same filters |
| Entry history | Every correction of an entry keeps the version it replaced - its day, hours, project and description, and when it was replaced - whether it was made in the form, by moving the entry to another project, on the week grid or for a whole selection at once. The entry's owner opens it from the entry; deleting the entry deletes its history |
| Hour budgets | A project may carry the hours its owner means to spend on it; the list shows how much of that is booked, marked from four fifths and past the whole, and the booking that crosses either says so |
| Templates | A booking made often - a project, hours, a description - is kept from the form and fills it in again when chosen. Nothing is booked from a template on its own, and one whose project has closed can no longer be chosen |
| Own statistics | Hours per day, per project and per activity, drawn as charts, for everybody rather than administrators |
| Activities | A second way of sorting the same hours beside the project, written into the description with a # - #meeting, #review - and totalled under *Own statistics*. Nothing to set up first, and every description already written counts; an entry naming two counts towards both |
| Evaluation | The same figures as bars, columns or a pie — whichever shape answers the question being asked |
| Overtime | Balance per day and per period against a personal daily target, and a running balance with bounds of your own that raise a banner while it lies past them |
| Calendar | Month view of where hours were booked |
| Working time | When each day began and finished and how long its breaks were, beside the hours booked on it, under the calendar - a day booked on with no times recorded is shown as the gap it is, the running stopwatch's start is offered as the day's, a day past a limit of the Working Hours Act says which, and the month leaves as a PDF. Kept by each person for themselves; nobody else sees it |
| Calendar subscription | An address of your own, made under *My account*, that Outlook, Google or a telephone subscribes to: every day you booked on - how many hours, and what on - and every day you are away, in the year either side of today. Read-only, shown once, and taken back by making a new one |
| Everything about me | Everything the installation keeps about your account, as a ZIP with a JSON file per part - exactly what erasing it would delete, without the hashes, seeds and keys - from *My account*, for anybody signed in |
| The year | Every day of a year as a tile, as dark as the hours booked on it, with the public holidays and the days away outlined rather than told by colour alone - beside the year before up to the same day, and both month by month as figures |
| Week grid | A week as a table - projects as rows, days as columns, hours typed straight into the cells - with what was booked, what each day asked and the balance underneath. *Copy last week* books the week before into the days that have nothing on them yet, leaving out what was booked on projects that have closed since |
| Days away | Leave, half a day of it, sick days, days taken against overtime and holidays that hold only where somebody works, recorded over a range - but only on the days the planned week asks something of, so a fortnight of leave is its ten working days - and named in the calendar and on the week grid. From the day every working day counts from, each takes its part off what the day asks |
| Leave | Days of leave a year, from a year on, and for each year what is left of them: what the year gives, what the year before left over - carried in, or owed where it was overdrawn - and what is taken and still planned, counted from the days of leave recorded on working days |
| Public holidays | The public holidays of every German state, worked out in the binary rather than fetched from anywhere. The installation chooses a state under *Settings*, and anybody can choose another - or none - under *My account*. They are named in the calendar and on the week, leave is not recorded on them, and from the day every working day counts from they ask nothing |
| Transport | Optional HTTPS with automatic Let's Encrypt certificates |
| Installable | A manifest and icons per installation - its own name and logo - so the page installs as an app on a telephone or a desktop, with shortcuts to the time entries, the calendar and the overtime. Browsers offer this over HTTPS or on localhost only |
| Operations | Health and liveness endpoints, Prometheus metrics, tracing (all from GoFr), administered under *Settings* |

## Projects

Projects are optional on a time entry: hours can be recorded first and
categorised later, or left uncategorised. The entry list counts the ones still
waiting for a project and leads to them, and several are given a project at once
from the bar that appears when rows are ticked - each through the same check a
single entry's transfer makes.

**A project belongs to one person.** Only its owner sees it, only its owner books
on it, and that includes the administrator — whose own projects are equally its own
and nobody else's. Two people working on the same thing therefore keep a project
each, with the same name if they like, and nothing adds them together, because
nothing is meant to.

There were two kinds until recently: a shared project everybody could see, and a
private category for organising your own day. The second is the only kind now, and
with it went the second right — `projects:write:own` and `projects:write` were two
rights for two kinds of project, so one of them would have granted nothing. Whoever
held the old one holds `projects:write`, which an upgrade takes care of.

A start date is optional, for the same reason: a project is one person's way of
organising their hours, not a plan somebody signed off.

That stays true of an **hour budget**, which a project may carry: how many hours
its owner means to spend on it, set by them, seen by nobody else and signed off by
nobody. The project list says how much of it the hours booked so far use, and marks
it from four fifths and again past the whole; the booking that takes a project past
either says so once, and the evaluation of one project names its budget beside its
total. A budget of nothing takes it off.

## Overtime

Every user has a **daily target** (the basis for overtime) and a **daily maximum**
(the booking limit); both are set under *My account*, by the person they are about and
by nobody else. Without a personal setting, 8 h target and the instance-wide
`MAX_DAILY_HOURS` apply.

The two ceilings are not alternatives: the **stricter** one holds. You may hold your
own day shorter than the installation allows, because that is your time; you cannot
raise it past what the installation allows, because that ceiling is configuration and
configuration is the administrator's.

The balance is the sum of `booked − target` over the days that count, and which
days count is each person's choice of a day under *My account*. Until one is
chosen, only the days that **have bookings** count — the rule from before there
was a holiday and leave calendar, kept so that a month already behind somebody
does not turn into a deficit the moment holidays and absences become known.
From the chosen day on, **every working day** up to today counts, booked or not,
and asks what its week plans less what a public holiday or an absence takes off
it: nothing on a public holiday, a day of leave, a sick day or a holiday that
holds only where somebody works; half on half a day of leave; the whole day on
a day off against overtime, which is what takes it off the balance. A day after
today counts once something is booked on it. The overtime table names the
holiday or the absence on each day that asks less, and says which rule counted.

What a day asks is the **week planned** for it. Under *My account* a week is
planned as seven figures, Monday first, from a day on: a later plan takes over
from its own day, and the weeks before it keep the plan that was in force then,
so going part time does not rewrite last month. Without a plan the daily target
applies from Monday to Friday and the weekend asks nothing - so hours worked on a
Saturday count as overtime. Every booked day used to be measured against the full
daily target whichever day it was, which stood a Saturday's work in the balance as
a shortfall.

The **running balance** is the same sum from the first day that counts - the first
day booked on, or the chosen day where that is earlier - up to today, as a
flexitime account keeps it. Under *My account* each person can give it two
**bounds**, an upper one at zero or above and a lower one at zero or below; while
the running balance lies past one of them, a banner says so on every screen, with
the way back to the bounds inside it, and goes once the balance is inside again.
Standing exactly on a bound is inside it, and an empty field is no bound.

The same rules decide the **reminder** about a day with nothing booked on it. It
names the last day before today somebody was expected to work - their week plans
hours for it, it is no public holiday where they work, and they were not away,
except for half a day of leave - and only that day: somebody who books a week at a
time is told about Thursday on Friday morning, and an earlier gap is the business
of the day it was. A day off against overtime is passed over, though it asks its
whole target of the balance, because nobody is meant to book on it. Nothing is said
about a day before the balance begins. The reminder is on until its owner switches
it off under *My account*, beside the hours after which a running stopwatch is
mentioned: ten, the most the Working Hours Act allows in a day, unless they choose
otherwise, and never for zero. A browser that allows it shows a notification as
well when the stopwatch crosses those hours while the page is open; one found
already past them on loading gets the banner alone.

It is your own balance. Somebody else's is their recorded time, totalled, so it
takes the same right as reading their entries — which only a manager holds, for the
accounts assigned to them, and then with every day away told as away, never why. The
team-wide overview that used to sit beside it is gone, because comparing colleagues
is the one thing this arrangement says nobody does.

## Timezones

An administrator sets one **instance-wide** zone under *Settings*; an
individual can override it under *My account*. An empty personal setting means
"follow the instance", which is the normal case — that way changing the
instance zone moves everyone who has not deliberately opted out.

This is not a display detail. The zone decides which **calendar day** a booking
falls on, so getting it wrong moves recorded hours between days and quietly
changes month-end totals. Zones are validated when saved, because a plausible
but wrong name like `Europe/Munich` would otherwise be stored and then fall
back to UTC at every use, with nothing on screen to show it.

The tz database is compiled into the binary (`time/tzdata`), so zones resolve
identically on a scratch or distroless container with no zoneinfo files of its
own.

## Your own record of your working time

Hours booked on projects say what the time went on; they do not say when it was
spent, and since the Federal Labour Court's decision of 13 September 2022 (1 ABR
22/21) a record of working time in Germany has to show when each day began and
ended. Under the calendar, *Working time* keeps that: for each day, when it began,
when it finished and how long its breaks were, as the clock showed them, with what
that comes to and the hours booked on projects beside it. A day booked
on with nothing recorded is listed too, so a month with a gap in it shows the gap.
A stopwatch running on the day offers its start as the day's, and the month leaves
as a PDF.

It is each person's own record, kept for themselves. There is no view of anybody
else's working time — not even a manager's, who looks after somebody's hours and
never their start, end and breaks — so this documents one's own time rather than
serving as an employer's time-keeping system.
A night that runs past midnight is recorded as the two days it falls on.

Each day is read against the three limits of the Working Hours Act
(Arbeitszeitgesetz) that a record of start, end and breaks can show, and a day past
one says which, in its own column and in the PDF:

| Limit | Section | Marked when |
| --- | --- | --- |
| Ten hours a day | § 3 | more than ten hours were worked |
| Breaks | § 4 | the breaks were shorter than 30 minutes past six hours of work, or 45 past nine |
| Rest | § 5 | fewer than eleven hours lay between the end of one shift and the start of the next |

A night recorded as the two days it falls on counts as one shift, so eight hours
from ten at night are a shift that needed a break rather than two short days with
no rest between them. The marks are notices and never refusals: the act has
exceptions - a hospital, a harvest, a collective agreement - that this application
cannot know about, and a record that refused the truth would be a worse record.

## Your days in your own calendar

Under **My account**, *Calendar subscription* makes an address a calendar
program subscribes to - Outlook, Google Calendar, the calendar on a telephone.
Every day you booked hours on is a whole day there saying how many, with each
entry written under it, and every day you recorded yourself away is one saying
why: the year either side of today, in the language you read the application
in. Every event is marked free, so booked hours never make you look busy to
whoever reads your free time.

A calendar program can send nothing but the address, so the key is in the
address, and that decides the rest:

- **It is a key of its own, not an API token.** It reads your days and nothing
  else, and never more than you could read on your own screen: it stops
  answering when your role no longer lets you read your own entries, and while a
  reset password waits to be chosen. Whatever the reason, it answers 404 and
  says nothing more.
- **It is shown once.** Only a hash of it is kept, as with a token. Making a new
  address takes the old one back at once, and so does removing it - which is
  what to do if anybody may have seen it. The card says when the address was
  last read, which is how to tell whether a calendar is reading it at all.
- **It is hidden in the log.** Every reading is logged under its address, so the
  process log writes `gtrcal_[hidden]` in place of the key - on the console as
  well as in the log viewer - and whoever reads the log learns that a calendar
  was read, not how to read it. Traces and metrics name the route, never the
  address.

Google Calendar and Outlook on the web fetch the address from their own
servers, so for them the installation has to be reachable from the internet; a
calendar on a telephone or a computer fetches it from wherever that device is,
over a VPN for example. How often it is fetched again is the calendar program's
choice: the feed asks for every hour, and Google takes up to a day whatever it
is asked.

## Everything about you

Under **My account**, *Everything about me* downloads what this installation keeps
about your account as a ZIP: a JSON file per part - the account, your entries and
the versions corrections replaced, projects, templates, absences, leave, planned
weeks, working times, the stopwatch, and the personal tokens, passkeys, sessions,
calendar address and provider links you have - each an array of rows with the
columns as they are stored, and `about.json` saying what the archive is. It is the
right of access and of portability (GDPR articles 15 and 20), open to anybody
signed in whatever their role lets them do here, and only ever about their own
account.

It holds exactly what erasing the account would delete, read from the same list
of tables, so nothing can be erased that could not have been handed over first.
What it leaves out is what would be a way in for whoever finds the file: the
password's hash, the second factor's seed, the hashes of tokens, sessions and the
calendar address, and a passkey's key material. About those it says that they
exist, when they were made and when they were last used.

## Spreadsheets

Every table that holds something goes out and comes back as a real **.xlsx**
workbook, from the tab it belongs to: time entries with the date, the person, the
project, the hours and the description; projects with their period and status;
accounts with their role and whether the password lives in the directory; roles
with a column per permission holding yes or no. Names rather than identifiers,
because a column of user ids is not something anybody can fill in by hand; hours
as a number, so the column can be totalled in Excel.

A column per right rather than one cell listing them, which is what makes the
roles sheet honest. A list in a cell reads well and imports badly: a typo in
`projects:read, projects:wrote` is a right silently dropped, and nothing about
it looks wrong until somebody cannot open a screen. A column is a question with
two answers, and a heading naming a right this application does not enforce is
refused by that name — which also catches a file exported from a different
installation. A file may leave rights out; a column somebody deleted is simply
a right the import does not touch.

Two tables deliberately have no sheet. A token's secret exists once, at the
moment it is created, and is not in the database to export. A passkey is bound
to the device holding it and means nothing anywhere else.

The column headings are written in the language the export was asked for, and a file
exported in any of them imports again: the heading row is skipped by position, and
translated values are recognised in every language.

Not comma-separated text. A CSV is what Excel mangles: the separator depends on the
machine's locale, dates are re-interpreted on opening, and a description containing
a semicolon quietly becomes two columns — none of it recoverable once somebody has
saved over the file.

The export follows the filters on screen and the same scoping the entry list uses,
so it can never show more than the screen did.

The import is **two steps, and all or nothing**:

1. *Check the file* reads it and shows every row — which would be written, which
   would not, and why, named by its line in the sheet so you know where to look. It
   writes nothing.
2. *Import* is only offered when every row can be written, and runs in one
   transaction.

A file half-imported leaves nobody able to say which half, or which entries came
from it, which is why a single refused row refuses the file. Every row goes through
the rules the API enforces — by calling them, not by restating them: the same
validation, the same daily ceiling (counting the file against itself, so forty rows
on one day are checked together), the same project visibility. A row naming somebody
else needs the right to book for others.

An import of time entries **creates**; it never updates or replaces, because there is
no way for it to know which existing entry a row was meant to be. Projects are
matched by name, so importing the same file twice changes nothing the second time.
Accounts are matched on the mail address and are **changed**, not created: a new
account needs a password, and a password that arrives in a spreadsheet is a password
that gets mailed around. Roles are matched on the name and are both — everything a
role is fits in a row, so a file can describe one that does not exist yet without
carrying anything that has no business being in a spreadsheet. The `admin` role is
the exception the role editor already makes: its rights cannot be changed from a
cell any more than from the screen.

Why a row was refused is written in the reader's language, like the headings above it
and the values in it. The reasons are a fixed set, so each one travels as a code with
whatever its sentence names — the row number, the offending word — rather than as a
sentence assembled on the server in English.

## The logo, and what a tab does with it

A logo is a **PNG or a JPEG**, up to 256 KB. Not an SVG: the same file that
renders perfectly in the header and on the sign-in screen can be refused as a tab
icon, silently, by an engine with its own rules about what it will rasterise —
nothing in the response says so, and the tab is simply empty.

**It is converted when it is saved**, into the size each place draws it:

| | Kept at | Drawn at |
| --- | --- | --- |
| Header | 440×80 | 220×40 |
| Sign-in card | 656×192 | 328×96 |
| Browser tab | 64×64, square | 16, 32 or more |

Twice the drawn size, because a screen at twice the density draws twice as many
pixels for the same box. Derived once and stored, so a page load reads them
rather than making them, and a restart has nothing to redo.

That matters most for the tab. What an installation uploads is a wordmark made
for a header — a few thousand pixels across, often twice as wide as it is tall —
and handed to a browser at that size, every decision is the browser's: whether to
accept it at all, how to fit two-to-one into a square, whether to bother. The
answers differ by engine, and one that decides against shows nothing and says
nothing. The tab's version is padded out to a square for the same reason; the
other two are not, because a header places the mark itself.

It also shrinks what every visitor downloads. The sign-in screen used to receive
the whole original inside the branding response, before anybody had signed in.

**Which part of the logo each place uses is yours to choose.** Press a preview on
the appearance screen and drag: any part, in any shape, from any corner. The
selection opens on the shape of the place it is for, because that is the one
selection nothing has to be done to, but nothing holds it there — and a part that
is not that shape is fitted into the place rather than stretched to fill it. A
wide header can take the whole wordmark; a browser tab has sixteen pixels, and the part worth
keeping there is usually the mark at one end rather than whatever falls in the
middle. Nothing has to be chosen — a logo starts as all of itself in all three
places, which is what most installations will leave it as.

The choice is remembered per place and per logo. Uploading a different logo
forgets it, because a selection made against one picture means nothing against
another.

Fitted, not cropped: nothing of the logo is cut. That does mean a wide wordmark
becomes a thin strip at the sixteen pixels a tab draws, which is a fact about
wordmarks rather than about this. The appearance screen shows the logo at exactly
that size beside the header and sign-in previews, so it is seen before saving. A
square mark is what reads best there — many companies keep one for exactly this.

**Until a logo is configured, the header and the sign-in card carry nothing.**
Those two slots belong to whoever runs the installation, and filling them with
our mark makes an unbranded installation look branded by somebody else. The
application's own mark — an hourglass — has its own places: the browser tab, the
`.exe`, and the button beside the title that leads to the welcome screen. It is
drawn (`internal/interface/web/assets/favicon.svg`) rather than a font character,
and `go run ./build/icon > build/icon.ico` redraws the Windows copy from the same
geometry whenever it changes.

**The tab can be named separately from the header**, under *Browser tab* beside
the title. The two are one name until the header's is too long to be a tab: a
browser cuts a tab off after a couple of dozen characters, and somebody has six
of them open — "Zeiterfassung der Beispiel GmbH & Co. KG" reads as
"Zeiterfassung der B…" in every one. Left empty, the tab keeps the title, which
is what every installation configured before this existed goes on doing. It
translates like the other texts.

**A language you have written nothing for follows the base**, the same way an
unconfigured logo falls back to the shipped mark. The form fills each language
with the base text so you can see what a reader of it currently gets; leaving
that untouched means exactly what it looks like — nothing written here. Rename
the installation later and every untranslated language follows along.

Saving a mark reloads the page, because no engine takes a new tab icon from a
link swapped in afterwards — and the reload comes back to where you were on the
screen rather than to the top of it.

## Business rules

The server enforces these; the interface merely also hides what is not allowed:

- A time entry belongs to the account it was booked for. Reading, editing, deleting,
  transferring and totalling it are refused to everybody else — including the account
  that administers the installation and the combined role — except a manager that
  account was assigned to, while the installation has managers, and then in the open:
  the entry names the manager who booked or last changed it. There is no state to
  travel through and nobody to approve anything: entries were once
  `open → submitted → approved`, and the role that approved them is gone.
- No form asks which person. The booking form, the entry filter, the calendar and
  the overtime form each had a dropdown of colleagues; every one of them held a single
  name by the end, and a question with one answer is not a question. A manager has
  one choice instead, under *Whose time*, which turns every screen at once and lists
  only the accounts assigned to them.
- Hours are booked only onto **active** projects, whichever way they get there:
  booking, transferring, or editing an entry onto one.
- The personal daily maximum applies, falling back to `MAX_DAILY_HOURS`.
- Archiving requires a completed project with no open entries left.
- A project that still has time entries cannot be deleted.
- Email addresses are unique.
- The built-in administrator cannot be deleted or stripped of administration.
- A system role cannot be deleted or renamed, and its permissions cannot be changed —
  in either direction. Its description can.
- A role has to grant at least one permission. One that grants nothing can be assigned,
  and whoever holds it signs in to an interface with nothing on it, which reads as a
  broken installation rather than as a decision.
- A role still assigned to someone cannot be deleted.
- A project is invisible to everyone but its owner, and asking for one that is not
  yours answers `404` rather than `403`.
- A correction keeps what the entry said before it, every time something in it
  changes, and only the entry's owner and a manager assigned to them can read that
  history - anybody else is told the entry is not there. A version a manager's
  correction replaced names the manager. A save that changes nothing keeps nothing, and deleting an
  entry deletes its history with it.
