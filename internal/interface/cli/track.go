package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// usage is what track says about itself.
const usage = `go-time-recording track - book time from a terminal

  track                              on a terminal, a screen: the stopwatch
                                     running, today's entries, and a form for
                                     each of the commands below
  track status                       the stopwatch, and what today adds up to
  track start [project] [-m text]    start the stopwatch, on a project or on none
  track stop                         stop it, and book what it measured
  track switch [project] [-m text]   book what it measured, carry on on another
  track book <hours> [project] [-m text] [-d YYYY-MM-DD]
                                     book hours by hand: 1.5, 1,5 or 1:30
  track today                        today's entries and their total

  --lang <language>                  the language track speaks, such as de or
                                     en, wherever it stands on the line;
                                     GTR_LANG keeps one, and otherwise it is
                                     the account's, then the system's

The installation is GTR_URL, for example https://time.example.com, and the
account is the owner of the personal token in GTR_TOKEN - made under My account,
API tokens. A project is named as it is called, or by the start of its name.
`

// Run carries out "go-time-recording track" with the arguments after the word,
// and answers with the exit status a shell reads: 0 for done, 1 where the
// installation refused or could not be reached, and 2 for a command line that
// could not be read.
//
// The word alone opens the screen when the answer goes to a terminal, and says
// what track does when it goes anywhere else: a script that runs track bare
// gets the lines and the status it always had, rather than a screen waiting for
// keys nobody is there to press.
//
// The installation and the token come from the environment rather than from
// flags: a token on a command line lands in the shell's history and in every
// process listing on the machine. The language may come from either, because it
// is nobody's secret - and where neither names one, it is the one the account
// chose in the browser, so the terminal speaks what the screen does.
func Run(ctx context.Context, args []string, env func(string) string, out, errOut io.Writer) int {
	flag, args, named := languageFlag(args)
	lang, explicit, err := chooseLanguage(flag, env, systemLanguage)

	if !named {
		err = usageError(lang.text("track.lang.missing", "--lang needs a language after it: {0}", spoken()))
	}

	complain := &session{out: errOut, lang: lang}
	if err != nil {
		return complain.failed(err)
	}

	onScreen := len(args) == 0 && isTerminal(out)

	if !onScreen && (len(args) == 0 || slices.Contains([]string{"help", "-h", "--help"}, args[0])) {
		(&session{out: out, lang: lang}).say("%s", strings.TrimRight(lang.usage(), "\n"))

		if len(args) == 0 {
			return 2
		}

		return 0
	}

	base, token := strings.TrimSpace(env("GTR_URL")), strings.TrimSpace(env("GTR_TOKEN"))
	if base == "" || token == "" {
		complain.say("%s", lang.text("track.needsAddress", "track needs GTR_URL, the installation's address, and GTR_TOKEN, a personal token made under My account."))

		return 2
	}

	s := &session{api: newClient(base, token, lang), out: out, lang: lang}

	// Asked before anything else, so a failure is said once: the request that
	// would have been next goes to the same installation and would fail the same
	// way, after the same wait.
	if !explicit {
		chosen, err := s.accountLanguage(ctx)
		if err != nil {
			return complain.failed(err)
		}

		if chosen != "" && slices.Contains(i18n.Languages(), chosen) {
			s.lang = speaking(chosen)
			s.api.lang, complain.lang = s.lang, s.lang
		}
	}

	if onScreen {
		err = s.show(ctx)
	} else {
		err = s.carryOut(ctx, args)
	}

	if err == nil {
		return 0
	}

	return complain.failed(err)
}

// failed says why something could not be done, and answers with the exit status
// that tells a script which kind of failure it was: 2 for a command line that
// could not be read, 1 for anything the installation refused or could not
// answer.
func (s *session) failed(err error) int {
	s.say("%v", err)

	var unreadable usageError
	if errors.As(err, &unreadable) {
		return 2
	}

	return 1
}

// accountLanguage is the language the token's owner chose in the browser, and
// empty where they never chose one - read from /me, as the browser reads it.
func (s *session) accountLanguage(ctx context.Context) (string, error) {
	var me struct {
		User struct {
			Language string `json:"language"`
		} `json:"user"`
	}

	err := s.api.call(ctx, http.MethodGet, "/me", nil, &me)

	return me.User.Language, err
}

// carryOut does what one command line asks.
func (s *session) carryOut(ctx context.Context, args []string) error {
	line, err := s.lang.parse(args[1:])
	if err != nil {
		return err
	}

	switch args[0] {
	case "status":
		return s.status(ctx, line)
	case "start":
		return s.start(ctx, line)
	case "stop":
		return s.stop(ctx, line)
	case "switch":
		return s.switchTo(ctx, line)
	case "book":
		return s.book(ctx, line)
	case "today":
		return s.today(ctx, line)
	}

	return usageError(s.lang.text("track.unknownCommand", "track does not know \"{0}\"; track help says what it does", args[0]))
}

// isTerminal tells a terminal from a pipe or a file. Asked of the terminal
// itself rather than read off the file's mode, which calls /dev/null a
// character device too.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)

	return ok && term.IsTerminal(int(f.Fd()))
}

// usageError is a command line that cannot be read, as against a request the
// installation refused: a shell tells the two apart by the exit status.
type usageError string

func (e usageError) Error() string { return string(e) }

// commandLine is what follows a command: its words, and the two options a word
// can carry - a description, and a day.
type commandLine struct {
	words       []string
	description *string
	day         string
}

// parse reads the words after a command, with -m and -d wherever they stand:
// after a project's name as much as before it, which is where people type them.
func (l language) parse(args []string) (commandLine, error) {
	var line commandLine

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m", "--message":
			if i+1 == len(args) {
				return line, usageError(l.text("track.m.missing", "-m needs the text after it"))
			}

			i++
			text := args[i]
			line.description = &text
		case "-d", "--date":
			if i+1 == len(args) {
				return line, usageError(l.text("track.d.missing", "-d needs a day after it, as YYYY-MM-DD"))
			}

			i++
			line.day = args[i]
		default:
			line.words = append(line.words, args[i])
		}
	}

	return line, nil
}

// session is one run of track against one installation, and the language it
// speaks.
type session struct {
	api  *client
	out  io.Writer
	lang language
}

// say writes a line of the answer. What a terminal does with it is the
// terminal's business: a closed pipe is somebody who stopped reading, not a
// failure to report to them.
func (s *session) say(format string, args ...any) {
	_, _ = fmt.Fprintf(s.out, format+"\n", args...)
}

// report prints what a change did, or hands on why it could not be made.
func (s *session) report(said string, err error) error {
	if err != nil {
		return err
	}

	s.say("%s", said)

	return nil
}

// project is one of the token owner's projects, as /projects lists it.
type project struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// entry is a time entry, as the API answers with one.
type entry struct {
	ProjectID     *uint   `json:"projectId"`
	Date          string  `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
}

// stopwatch is the running clock, as /me/timer answers.
type stopwatch struct {
	Running      bool       `json:"running"`
	ProjectID    *uint      `json:"projectId"`
	Description  *string    `json:"description"`
	StartedAt    *time.Time `json:"startedAt"`
	ElapsedHours float64    `json:"elapsedHours"`
}

// me is the token's owner as /me answers: what they are called, and where they
// keep their days - their own zone, or the installation's where they chose
// none, worked out by the installation so that a day here is the day the
// browser would show them.
func (s *session) me(ctx context.Context) (string, *time.Location, error) {
	var me struct {
		User struct {
			Name              string `json:"name"`
			EffectiveTimezone string `json:"effectiveTimezone"`
		} `json:"user"`
	}

	if err := s.api.call(ctx, http.MethodGet, "/me", nil, &me); err != nil {
		return "", nil, err
	}

	zone, err := time.LoadLocation(me.User.EffectiveTimezone)
	if err != nil || me.User.EffectiveTimezone == "" {
		zone = time.UTC
	}

	return me.User.Name, zone, nil
}

// zone is where the token's owner keeps their days.
func (s *session) zone(ctx context.Context) (*time.Location, error) {
	_, zone, err := s.me(ctx)

	return zone, err
}

// projects lists the token owner's projects.
func (s *session) projects(ctx context.Context) ([]project, error) {
	var listed struct {
		Items []project `json:"items"`
	}

	err := s.api.call(ctx, http.MethodGet, "/projects", nil, &listed)

	return listed.Items, err
}

// snapshot is the token owner's day as it stands: who they are, where their
// days are kept, their projects, the stopwatch, and what today has booked.
type snapshot struct {
	name     string
	zone     *time.Location
	projects []project
	clock    stopwatch
	today    []entry
}

// snapshot reads the token owner's day.
func (s *session) snapshot(ctx context.Context) (snapshot, error) {
	var now snapshot

	name, zone, err := s.me(ctx)
	if err != nil {
		return now, err
	}

	now.name, now.zone = name, zone

	if now.projects, err = s.projects(ctx); err != nil {
		return now, err
	}

	if err := s.api.call(ctx, http.MethodGet, "/me/timer", nil, &now.clock); err != nil {
		return now, err
	}

	now.today, err = s.entriesOn(ctx, time.Now().In(zone).Format(time.DateOnly))

	return now, err
}

// clockLine says what the stopwatch is doing, given how long it has run.
func (n snapshot) clockLine(l language, elapsed float64) string {
	if !n.clock.Running || n.clock.StartedAt == nil {
		return l.text("track.notRunning", "The stopwatch is not running.")
	}

	return l.text("track.running", "Running since {0} on {1} - {2} so far.", n.clock.StartedAt.In(n.zone).Format("15:04"),
		l.nameOf(n.projects, n.clock.ProjectID), hoursAndMinutes(elapsed))
}

// projectNamed finds the active project somebody means: the one called that,
// whatever the case, or else the only one whose name starts with it.
func (l language) projectNamed(projects []project, name string) (project, error) {
	wanted := strings.ToLower(strings.TrimSpace(name))

	var starting []project

	for _, p := range projects {
		if p.Status != "active" {
			continue
		}

		if strings.ToLower(p.Name) == wanted {
			return p, nil
		}

		if strings.HasPrefix(strings.ToLower(p.Name), wanted) {
			starting = append(starting, p)
		}
	}

	switch len(starting) {
	case 1:
		return starting[0], nil
	case 0:
		return project{}, errors.New(l.text("track.noSuchProject", "no active project of yours is called \"{0}\"", name))
	}

	names := make([]string, 0, len(starting))
	for _, p := range starting {
		names = append(names, p.Name)
	}

	return project{}, errors.New(l.text("track.ambiguous", "\"{0}\" could be any of {1}; say which", name, strings.Join(names, ", ")))
}

// nameOf is what a project is called, by its id; nil is no project.
func (l language) nameOf(projects []project, id *uint) string {
	if id == nil {
		return l.text("track.noProject", "no project")
	}

	at := slices.IndexFunc(projects, func(p project) bool { return p.ID == *id })
	if at < 0 {
		return l.text("track.projectNumber", "project {0}", strconv.FormatUint(uint64(*id), 10))
	}

	return projects[at].Name
}

// aimedAt is the start of a request for the stopwatch: the project its words
// name, if they name one, and the description given.
func (s *session) aimedAt(ctx context.Context, line commandLine) (map[string]any, string, error) {
	body := map[string]any{}
	name := s.lang.text("track.noProject", "no project")

	if len(line.words) > 0 {
		projects, err := s.projects(ctx)
		if err != nil {
			return nil, "", err
		}

		chosen, err := s.lang.projectNamed(projects, strings.Join(line.words, " "))
		if err != nil {
			return nil, "", err
		}

		body["projectId"], name = chosen.ID, chosen.Name
	}

	if line.description != nil {
		body["description"] = *line.description
	}

	return body, name, nil
}

// The changes track makes. Each answers with what it did in a sentence, which
// the command prints and the screen shows under the clock - the same words for
// the same change, wherever it was asked for.

// startClock starts the stopwatch as body says, on the project called name, and
// says when it started in the owner's zone.
func (s *session) startClock(ctx context.Context, body map[string]any, name string, zone *time.Location) (string, error) {
	var clock stopwatch
	if err := s.api.call(ctx, http.MethodPost, "/me/timer", body, &clock); err != nil {
		return "", err
	}

	started := time.Now()
	if clock.StartedAt != nil {
		started = *clock.StartedAt
	}

	return s.lang.text("track.started", "Started at {0} on {1}.", started.In(zone).Format("15:04"), name), nil
}

// stopClock stops the stopwatch and books what it measured.
func (s *session) stopClock(ctx context.Context, projects []project) (string, error) {
	var booked entry
	if err := s.api.call(ctx, http.MethodPost, "/me/timer/stop", nil, &booked); err != nil {
		return "", err
	}

	return s.lang.text("track.stopped", "Booked {0} on {1}.", s.lang.hours(booked.DurationHours),
		s.lang.nameOf(projects, booked.ProjectID)), nil
}

// switchClock books what the stopwatch measured and carries on as body says, on
// the project called name.
func (s *session) switchClock(ctx context.Context, body map[string]any, name string, projects []project) (string, error) {
	var switched struct {
		Booked entry `json:"booked"`
	}

	if err := s.api.call(ctx, http.MethodPost, "/me/timer/switch", body, &switched); err != nil {
		return "", err
	}

	return s.lang.text("track.switched", "Booked {0} on {1}; running on {2} now.", s.lang.hours(switched.Booked.DurationHours),
		s.lang.nameOf(projects, switched.Booked.ProjectID), name), nil
}

// bookHours books hours by hand as body says, on the project called name.
func (s *session) bookHours(ctx context.Context, body map[string]any, name string) (string, error) {
	var booked entry
	if err := s.api.call(ctx, http.MethodPost, "/timesheets", body, &booked); err != nil {
		return "", err
	}

	return s.lang.text("track.booked", "Booked {0} on {1} for {2}.", s.lang.hours(booked.DurationHours), name,
		s.lang.date(booked.Date)), nil
}

func (s *session) status(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 {
		return usageError(s.lang.text("track.status.extra", "status takes nothing after it"))
	}

	now, err := s.snapshot(ctx)
	if err != nil {
		return err
	}

	s.say("%s", now.clockLine(s.lang, now.clock.ElapsedHours))
	s.say("%s", s.lang.text("track.bookedToday", "Booked today: {0}.", s.lang.hours(total(now.today))))

	return nil
}

func (s *session) start(ctx context.Context, line commandLine) error {
	zone, err := s.zone(ctx)
	if err != nil {
		return err
	}

	body, name, err := s.aimedAt(ctx, line)
	if err != nil {
		return err
	}

	return s.report(s.startClock(ctx, body, name, zone))
}

func (s *session) stop(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 || line.description != nil || line.day != "" {
		return usageError(s.lang.text("track.stop.extra", "stop takes nothing after it: the stopwatch already knows what it is on"))
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	return s.report(s.stopClock(ctx, projects))
}

func (s *session) switchTo(ctx context.Context, line commandLine) error {
	body, name, err := s.aimedAt(ctx, line)
	if err != nil {
		return err
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	return s.report(s.switchClock(ctx, body, name, projects))
}

func (s *session) book(ctx context.Context, line commandLine) error {
	if len(line.words) == 0 {
		return usageError(s.lang.text("track.book.noHours", "book needs the hours first: track book 1.5 Website"))
	}

	worked, err := s.lang.parseHours(line.words[0])
	if err != nil {
		return err
	}

	day := line.day
	if day == "" {
		zone, err := s.zone(ctx)
		if err != nil {
			return err
		}

		day = time.Now().In(zone).Format(time.DateOnly)
	} else if err := s.lang.readableDay(day); err != nil {
		return err
	}

	body, name, err := s.aimedAt(ctx, commandLine{words: line.words[1:], description: line.description})
	if err != nil {
		return err
	}

	body["date"], body["durationHours"] = day, worked

	return s.report(s.bookHours(ctx, body, name))
}

func (s *session) today(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 {
		return usageError(s.lang.text("track.today.extra", "today takes nothing after it"))
	}

	zone, err := s.zone(ctx)
	if err != nil {
		return err
	}

	entries, err := s.entriesOn(ctx, time.Now().In(zone).Format(time.DateOnly))
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		s.say("%s", s.lang.text("track.nothingToday", "Nothing booked today."))

		return nil
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	sum := s.lang.hours(total(entries))
	width := hoursColumn(append(s.lang.hoursOf(entries), sum)...)

	for _, e := range entries {
		text := ""
		if e.Description != nil && *e.Description != "" {
			text = " - " + *e.Description
		}

		s.say("   %*s  %s%s", width, s.lang.hours(e.DurationHours), s.lang.nameOf(projects, e.ProjectID), text)
	}

	s.say("   %*s  %s", width, sum, s.lang.text("track.inTotal", "in total"))

	return nil
}

// hoursOf is the hours of each entry, written out.
func (l language) hoursOf(entries []entry) []string {
	written := make([]string, len(entries))

	for i, e := range entries {
		written[i] = l.hours(e.DurationHours)
	}

	return written
}

// hoursColumn is how wide a column of hours is: as wide as the widest written,
// and never narrower than six - an English day's hours, which is the width the
// column had before it was written in German too.
func hoursColumn(written ...string) int {
	width := 6

	for _, w := range written {
		width = max(width, utf8.RuneCountInString(w))
	}

	return width
}

// entriesOn reads the entries of one day - a page holds far more than a day's.
func (s *session) entriesOn(ctx context.Context, day string) ([]entry, error) {
	query := url.Values{"from": {day}, "to": {day}, "limit": {"500"}}

	var listed struct {
		Items []entry `json:"items"`
	}

	err := s.api.call(ctx, http.MethodGet, "/timesheets?"+query.Encode(), nil, &listed)

	return listed.Items, err
}

// parseHours reads hours the ways people write them: 1.5, 1,5 or 1:30.
func (l language) parseHours(raw string) (float64, error) {
	refused := usageError(l.text("track.notHours", "\"{0}\" is not a number of hours: write 1.5, 1,5 or 1:30", raw))

	if whole, minutes, found := strings.Cut(raw, ":"); found {
		h, errHours := strconv.Atoi(whole)
		m, errMinutes := strconv.Atoi(minutes)

		if errHours != nil || errMinutes != nil || h < 0 || m < 0 || m >= 60 || h+m == 0 {
			return 0, refused
		}

		return float64(h) + float64(m)/60, nil
	}

	worked, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64)
	if err != nil || math.IsNaN(worked) || math.IsInf(worked, 0) || worked <= 0 {
		return 0, refused
	}

	return worked, nil
}

// readableDay refuses a day that is not written as YYYY-MM-DD, which is the one
// way the API reads one.
func (l language) readableDay(day string) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return usageError(l.text("track.notDay", "\"{0}\" is not a day: write it as YYYY-MM-DD", day))
	}

	return nil
}

// total adds up what entries booked.
func total(entries []entry) float64 {
	var sum float64

	for _, e := range entries {
		sum += e.DurationHours
	}

	return sum
}

// hoursAndMinutes writes a running clock's time as a clock would: 1:23.
func hoursAndMinutes(h float64) string {
	minutes := int(math.Round(h * 60))

	return fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
}
