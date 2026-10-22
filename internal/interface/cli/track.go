package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// usage is what track says about itself.
const usage = `go-time-recording track - book time from a terminal

  track status                       the stopwatch, and what today adds up to
  track start [project] [-m text]    start the stopwatch, on a project or on none
  track stop                         stop it, and book what it measured
  track switch [project] [-m text]   book what it measured, carry on on another
  track book <hours> [project] [-m text] [-d YYYY-MM-DD]
                                     book hours by hand: 1.5, 1,5 or 1:30
  track today                        today's entries and their total

The installation is GTR_URL, for example https://time.example.com, and the
account is the owner of the personal token in GTR_TOKEN - made under My account,
API tokens. A project is named as it is called, or by the start of its name.
`

// Run carries out "go-time-recording track" with the arguments after the word,
// and answers with the exit status a shell reads: 0 for done, 1 where the
// installation refused or could not be reached, and 2 for a command line that
// could not be read.
//
// The installation and the token come from the environment rather than from
// flags: a token on a command line lands in the shell's history and in every
// process listing on the machine.
func Run(ctx context.Context, args []string, env func(string) string, out, errOut io.Writer) int {
	complain := &session{out: errOut}

	if len(args) == 0 || slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		(&session{out: out}).say("%s", strings.TrimRight(usage, "\n"))

		if len(args) == 0 {
			return 2
		}

		return 0
	}

	base, token := strings.TrimSpace(env("GTR_URL")), strings.TrimSpace(env("GTR_TOKEN"))
	if base == "" || token == "" {
		complain.say("track needs GTR_URL, the installation's address, and GTR_TOKEN, " +
			"a personal token made under My account.")

		return 2
	}

	line, err := parse(args[1:])
	if err == nil {
		s := &session{api: newClient(base, token), out: out}

		switch args[0] {
		case "status":
			err = s.status(ctx, line)
		case "start":
			err = s.start(ctx, line)
		case "stop":
			err = s.stop(ctx, line)
		case "switch":
			err = s.switchTo(ctx, line)
		case "book":
			err = s.book(ctx, line)
		case "today":
			err = s.today(ctx, line)
		default:
			err = usageError(fmt.Sprintf("track does not know %q; track help says what it does", args[0]))
		}
	}

	if err == nil {
		return 0
	}

	complain.say("%v", err)

	var unreadable usageError
	if errors.As(err, &unreadable) {
		return 2
	}

	return 1
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
func parse(args []string) (commandLine, error) {
	var line commandLine

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m", "--message":
			if i+1 == len(args) {
				return line, usageError("-m needs the text after it")
			}

			i++
			text := args[i]
			line.description = &text
		case "-d", "--date":
			if i+1 == len(args) {
				return line, usageError("-d needs a day after it, as YYYY-MM-DD")
			}

			i++
			line.day = args[i]
		default:
			line.words = append(line.words, args[i])
		}
	}

	return line, nil
}

// session is one run of track against one installation.
type session struct {
	api *client
	out io.Writer
}

// say writes a line of the answer. What a terminal does with it is the
// terminal's business: a closed pipe is somebody who stopped reading, not a
// failure to report to them.
func (s *session) say(format string, args ...any) {
	_, _ = fmt.Fprintf(s.out, format+"\n", args...)
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
	StartedAt    *time.Time `json:"startedAt"`
	ElapsedHours float64    `json:"elapsedHours"`
}

// zone is where the token's owner keeps their days: their own zone, or the
// installation's where they chose none - worked out by the installation, so a
// day here is the day the browser would show them.
func (s *session) zone(ctx context.Context) (*time.Location, error) {
	var me struct {
		User struct {
			EffectiveTimezone string `json:"effectiveTimezone"`
		} `json:"user"`
	}

	if err := s.api.call(ctx, http.MethodGet, "/me", nil, &me); err != nil {
		return nil, err
	}

	zone, err := time.LoadLocation(me.User.EffectiveTimezone)
	if err != nil || me.User.EffectiveTimezone == "" {
		return time.UTC, nil
	}

	return zone, nil
}

// projects lists the token owner's projects.
func (s *session) projects(ctx context.Context) ([]project, error) {
	var listed struct {
		Items []project `json:"items"`
	}

	err := s.api.call(ctx, http.MethodGet, "/projects", nil, &listed)

	return listed.Items, err
}

// projectNamed finds the active project somebody means: the one called that,
// whatever the case, or else the only one whose name starts with it.
func projectNamed(projects []project, name string) (project, error) {
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
		return project{}, fmt.Errorf("no active project of yours is called %q", name)
	}

	names := make([]string, 0, len(starting))
	for _, p := range starting {
		names = append(names, p.Name)
	}

	return project{}, fmt.Errorf("%q could be any of %s; say which", name, strings.Join(names, ", "))
}

// nameOf is what a project is called, by its id; nil is no project.
func nameOf(projects []project, id *uint) string {
	if id == nil {
		return "no project"
	}

	at := slices.IndexFunc(projects, func(p project) bool { return p.ID == *id })
	if at < 0 {
		return fmt.Sprintf("project %d", *id)
	}

	return projects[at].Name
}

// aimedAt is the start of a request for the stopwatch: the project its words
// name, if they name one, and the description given.
func (s *session) aimedAt(ctx context.Context, line commandLine) (map[string]any, string, error) {
	body := map[string]any{}
	name := "no project"

	if len(line.words) > 0 {
		projects, err := s.projects(ctx)
		if err != nil {
			return nil, "", err
		}

		chosen, err := projectNamed(projects, strings.Join(line.words, " "))
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

func (s *session) status(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 {
		return usageError("status takes nothing after it")
	}

	zone, err := s.zone(ctx)
	if err != nil {
		return err
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	var clock stopwatch
	if err := s.api.call(ctx, http.MethodGet, "/me/timer", nil, &clock); err != nil {
		return err
	}

	if clock.Running && clock.StartedAt != nil {
		s.say("Running since %s on %s - %s so far.", clock.StartedAt.In(zone).Format("15:04"),
			nameOf(projects, clock.ProjectID), hoursAndMinutes(clock.ElapsedHours))
	} else {
		s.say("The stopwatch is not running.")
	}

	entries, err := s.entriesOn(ctx, time.Now().In(zone).Format(time.DateOnly))
	if err != nil {
		return err
	}

	s.say("Booked today: %s.", hours(total(entries)))

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

	var clock stopwatch
	if err := s.api.call(ctx, http.MethodPost, "/me/timer", body, &clock); err != nil {
		return err
	}

	started := time.Now()
	if clock.StartedAt != nil {
		started = *clock.StartedAt
	}

	s.say("Started at %s on %s.", started.In(zone).Format("15:04"), name)

	return nil
}

func (s *session) stop(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 || line.description != nil || line.day != "" {
		return usageError("stop takes nothing after it: the stopwatch already knows what it is on")
	}

	var booked entry
	if err := s.api.call(ctx, http.MethodPost, "/me/timer/stop", nil, &booked); err != nil {
		return err
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	s.say("Booked %s on %s.", hours(booked.DurationHours), nameOf(projects, booked.ProjectID))

	return nil
}

func (s *session) switchTo(ctx context.Context, line commandLine) error {
	body, name, err := s.aimedAt(ctx, line)
	if err != nil {
		return err
	}

	var switched struct {
		Booked entry `json:"booked"`
	}

	if err := s.api.call(ctx, http.MethodPost, "/me/timer/switch", body, &switched); err != nil {
		return err
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	s.say("Booked %s on %s; running on %s now.", hours(switched.Booked.DurationHours),
		nameOf(projects, switched.Booked.ProjectID), name)

	return nil
}

func (s *session) book(ctx context.Context, line commandLine) error {
	if len(line.words) == 0 {
		return usageError("book needs the hours first: track book 1.5 Website")
	}

	worked, err := parseHours(line.words[0])
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
	} else if _, err := time.Parse(time.DateOnly, day); err != nil {
		return usageError(fmt.Sprintf("%q is not a day: write it as YYYY-MM-DD", day))
	}

	body, name, err := s.aimedAt(ctx, commandLine{words: line.words[1:], description: line.description})
	if err != nil {
		return err
	}

	body["date"], body["durationHours"] = day, worked

	var booked entry
	if err := s.api.call(ctx, http.MethodPost, "/timesheets", body, &booked); err != nil {
		return err
	}

	s.say("Booked %s on %s for %s.", hours(booked.DurationHours), name, booked.Date)

	return nil
}

func (s *session) today(ctx context.Context, line commandLine) error {
	if len(line.words) > 0 {
		return usageError("today takes nothing after it")
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
		s.say("Nothing booked today.")

		return nil
	}

	projects, err := s.projects(ctx)
	if err != nil {
		return err
	}

	for _, e := range entries {
		text := ""
		if e.Description != nil && *e.Description != "" {
			text = " - " + *e.Description
		}

		s.say("%9s  %s%s", hours(e.DurationHours), nameOf(projects, e.ProjectID), text)
	}

	s.say("%9s  in total", hours(total(entries)))

	return nil
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
func parseHours(raw string) (float64, error) {
	refused := usageError(fmt.Sprintf("%q is not a number of hours: write 1.5, 1,5 or 1:30", raw))

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

// total adds up what entries booked.
func total(entries []entry) float64 {
	var sum float64

	for _, e := range entries {
		sum += e.DurationHours
	}

	return sum
}

// hours writes a number of hours the way the screens do in English.
func hours(h float64) string { return fmt.Sprintf("%.2f h", h) }

// hoursAndMinutes writes a running clock's time as a clock would: 1:23.
func hoursAndMinutes(h float64) string {
	minutes := int(math.Round(h * 60))

	return fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
}
