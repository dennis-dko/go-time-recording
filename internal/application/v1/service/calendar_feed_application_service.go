package service

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
	"github.com/dennis-dko/go-time-recording/internal/support/ical"
	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// CalendarFeedKeyPrefix marks a key as a calendar feed's, so that it is
// recognisable wherever it turns up - and so that the process log can hide what
// follows it, because a subscription's address is written into the request log of
// everything it passes through.
const CalendarFeedKeyPrefix = "gtrcal_"

// calendarFeedYears is how far a feed reaches on each side of today: the year
// behind, as far back as anybody looks for a day they forgot to book, and the year
// ahead, where leave is planned.
const calendarFeedYears = 1

// CalendarName is what a feed is called in a language: the installation's own name,
// as its owner reads it.
type CalendarName func(ctx context.Context, language string) string

// CalendarFeedService gives each person an address their calendar program reads
// their recorded days from, so the hours they booked stand beside their
// appointments and a day with none shows as the gap it is.
//
// The address is reached by its own key rather than by a session or an API token:
// a calendar program can do nothing but fetch a URL, so the secret is in the URL,
// and the URL is written into logs. What the key opens is therefore exactly what
// the person could read on their own screen, read-only - and less, rather than
// more, whenever that changes.
type CalendarFeedService struct {
	feeds      repository.CalendarFeedRepository
	users      repository.UserRepository
	timesheets repository.TimesheetRepository
	absences   repository.AbsenceRepository
	projects   repository.ProjectRepository
	auth       *AuthService
	today      Today
	name       CalendarName
}

// NewCalendarFeedService needs the auth service because a key is never more than
// its owner, whose rights are those of their role at the moment the feed is read.
func NewCalendarFeedService(
	feeds repository.CalendarFeedRepository,
	users repository.UserRepository,
	timesheets repository.TimesheetRepository,
	absences repository.AbsenceRepository,
	projects repository.ProjectRepository,
	auth *AuthService,
	today Today,
	name CalendarName,
) *CalendarFeedService {
	return &CalendarFeedService{
		feeds: feeds, users: users, timesheets: timesheets, absences: absences, projects: projects,
		auth: auth, today: today, name: name,
	}
}

// IssuedCalendarFeed is a feed as it is created, which is the one time its key is
// known: only a hash of it is kept.
type IssuedCalendarFeed struct {
	Feed *model.CalendarFeed
	Key  string
}

// Create gives somebody a feed, in place of the one they had - whose address reads
// nothing from then on.
func (s *CalendarFeedService) Create(ctx context.Context, userID uint) (*IssuedCalendarFeed, error) {
	secret, err := security.NewSessionToken()
	if err != nil {
		return nil, apperror.Internal(err)
	}

	key := CalendarFeedKeyPrefix + secret

	feed := &model.CalendarFeed{
		UserID:    userID,
		KeyHash:   security.HashToken(key),
		Prefix:    key[:len(CalendarFeedKeyPrefix)+6],
		CreatedAt: time.Now(),
	}

	if err := s.feeds.Replace(ctx, feed); err != nil {
		return nil, err
	}

	return &IssuedCalendarFeed{Feed: feed, Key: key}, nil
}

// Feed returns somebody's feed, or nil when they have none - which is an answer
// rather than an error.
func (s *CalendarFeedService) Feed(ctx context.Context, userID uint) (*model.CalendarFeed, error) {
	feed, err := s.feeds.ForUser(ctx, userID)
	if apperror.KindOf(err) == apperror.KindNotFound {
		return nil, nil
	}

	return feed, err
}

// Remove takes somebody's feed away, so that its address reads nothing.
func (s *CalendarFeedService) Remove(ctx context.Context, userID uint) error {
	removed, err := s.feeds.Delete(ctx, userID)
	if err != nil {
		return err
	}

	if !removed {
		return apperror.NotFound("feed", strconv.FormatUint(uint64(userID), 10))
	}

	return nil
}

// Read writes the calendar a key opens: a whole day for every day its owner booked
// on in the year either side of today, saying how many hours and with what they
// booked written under it, and a whole day for every day they recorded themselves
// away. In the owner's language, which is the only one a calendar program cannot
// ask for.
func (s *CalendarFeedService) Read(ctx context.Context, key string) ([]byte, error) {
	feed, user, err := s.owner(ctx, key)
	if err != nil {
		return nil, err
	}

	today := todayOf(ctx, s.today, user)
	from, to := today.AddDate(-calendarFeedYears, 0, 0), today.AddDate(calendarFeedYears, 0, 0)

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID: user.ID, StartDate: &from, EndDate: &to,
	})
	if err != nil {
		return nil, err
	}

	away, err := s.absences.Between(ctx, user.ID, from, to)
	if err != nil {
		return nil, err
	}

	names, err := s.projectNames(ctx, user.ID, entries)
	if err != nil {
		return nil, err
	}

	language := user.EffectiveLanguage()
	words := feedWords{i18n.For(language)}

	calendar := ical.Calendar{Stamp: time.Now()}
	if s.name != nil {
		calendar.Name = s.name(ctx, language)
	}

	uid := func(day time.Time, what string) string {
		return day.Format("20060102") + "-" + what + "-" + feed.Prefix + "@go-time-recording"
	}

	for _, day := range bookedDays(entries) {
		total, lines := 0.0, make([]string, 0, len(day.entries))

		for _, entry := range day.entries {
			total += entry.DurationHours
			lines = append(lines, words.entryLine(entry, names))
		}

		calendar.Events = append(calendar.Events, ical.Event{
			UID:         uid(day.date, "booked"),
			Day:         day.date,
			Summary:     words.booked(total),
			Description: strings.Join(lines, "\n"),
		})
	}

	for _, absence := range away {
		date := model.CalendarDay(absence.Day)

		calendar.Events = append(calendar.Events, ical.Event{
			UID: uid(date, "away"), Day: date, Summary: words.absenceName(absence.Kind),
		})
	}

	// By day, and a day's hours before the reason it was away, so that the same
	// feed is written the same way twice.
	slices.SortStableFunc(calendar.Events, func(a, b ical.Event) int { return a.Day.Compare(b.Day) })

	return ical.Write(calendar), nil
}

// owner resolves a key to its feed and the account it reads.
//
// Not found for a key nobody was given, and just as much for one whose owner could
// not read the same days on their own screen: their role no longer lets them read
// their own entries, or their password was reset and waits to be chosen, which
// stops an API token too. A calendar program is not who to explain that to, and
// "not found" is the one answer that says nothing about the key.
func (s *CalendarFeedService) owner(ctx context.Context, key string) (*model.CalendarFeed, *model.User, error) {
	gone := apperror.NotFound("feed", "")

	key = strings.TrimSuffix(key, ".ics")
	if !strings.HasPrefix(key, CalendarFeedKeyPrefix) {
		return nil, nil, gone
	}

	feed, err := s.feeds.ByHash(ctx, security.HashToken(key))
	if err != nil {
		return nil, nil, missingOr(err, gone)
	}

	user, err := s.users.GetByID(ctx, feed.UserID)
	if err != nil {
		return nil, nil, missingOr(err, gone)
	}

	if user.MustChangePassword {
		return nil, nil, gone
	}

	principal, err := s.auth.principalFor(ctx, user)
	if err != nil {
		return nil, nil, err
	}

	if !principal.Can(model.PermTimesheetReadOwn) {
		return nil, nil, gone
	}

	// Best effort, as a token's last use is: it tells the person whether their
	// calendar is reading the address at all, and failing to record that must not
	// fail the reading.
	if feed.LastReadAt == nil || time.Since(*feed.LastReadAt) >= maxTouchInterval {
		_ = s.feeds.TouchLastRead(ctx, user.ID, time.Now())
	}

	return feed, user, nil
}

// projectNames names the projects the entries were booked on that their owner may
// see. An entry left on somebody else's project from the days projects were shared
// is written without the name, because to its owner that project does not exist.
func (s *CalendarFeedService) projectNames(
	ctx context.Context,
	userID uint,
	entries []*model.Timesheet,
) (map[uint]string, error) {
	names := make(map[uint]string)
	looked := make(map[uint]bool)

	for _, entry := range entries {
		if entry.ProjectID == nil || looked[*entry.ProjectID] {
			continue
		}

		looked[*entry.ProjectID] = true

		project, err := s.projects.GetByID(ctx, *entry.ProjectID)
		if apperror.KindOf(err) == apperror.KindNotFound {
			continue
		}

		if err != nil {
			return nil, err
		}

		if domainservice.RequireVisible(project, userID) == nil {
			names[project.ID] = project.Name
		}
	}

	return names, nil
}

// bookedDay is one day's entries.
type bookedDay struct {
	date    time.Time
	entries []*model.Timesheet
}

// bookedDays groups entries by the day they were booked on, earliest first and
// each day's in the order they were booked.
func bookedDays(entries []*model.Timesheet) []bookedDay {
	byDay := make(map[time.Time][]*model.Timesheet)

	for _, entry := range entries {
		date := model.CalendarDay(entry.Date)
		byDay[date] = append(byDay[date], entry)
	}

	days := make([]bookedDay, 0, len(byDay))

	for date, own := range byDay {
		slices.SortFunc(own, func(a, b *model.Timesheet) int { return cmp.Compare(a.ID, b.ID) })
		days = append(days, bookedDay{date: date, entries: own})
	}

	slices.SortFunc(days, func(a, b bookedDay) int { return a.date.Compare(b.date) })

	return days
}

// feedWords is how a feed writes in one language: in the interface's own words,
// from the dictionaries every part of the application reads, so the calendar and
// the screen call a thing the same - an hour, an entry with no project, a reason
// to be away. A language with no dictionary is written in English, which is
// what the fallbacks here say.
type feedWords struct {
	words i18n.Translator
}

// absenceEnglish is what each kind of day away is called in English: the
// interface's own fallbacks, which live in its script rather than in a
// dictionary - TestTheFeedCallsThingsWhatTheScreenCallsThem holds the two
// together.
var absenceEnglish = map[string]string{
	model.AbsenceVacation:     "Leave",
	model.AbsenceHalfVacation: "Half a day of leave",
	model.AbsenceSick:         "Sick",
	model.AbsenceFlexDay:      "Day off against overtime",
	model.AbsenceLocalHoliday: "Holiday where I work",
}

// hoursOf writes a number of hours the way the screen does: two places, in the
// reader's separators, followed by the unit.
func (w feedWords) hoursOf(hours float64) string {
	return w.words.Number(hours) + " " + w.words.Text("unit.hours", "h")
}

// booked is a day's summary: what it adds up to.
func (w feedWords) booked(hours float64) string {
	return w.words.Text("calendarFeed.booked", "{0} booked", w.hoursOf(hours))
}

// entryLine is one entry under its day: the hours, the project, and what was
// written about it.
func (w feedWords) entryLine(entry *model.Timesheet, names map[uint]string) string {
	parts := []string{w.hoursOf(entry.DurationHours)}

	if entry.ProjectID == nil {
		parts = append(parts, w.words.Text("ts.noProject", "No project"))
	} else if name, named := names[*entry.ProjectID]; named {
		parts = append(parts, name)
	}

	if entry.Description != nil {
		if description := strings.TrimSpace(*entry.Description); description != "" {
			parts = append(parts, description)
		}
	}

	return strings.Join(parts, " · ")
}

// absenceName is what a day away is called, or its kind as recorded for one
// nothing has a name for.
func (w feedWords) absenceName(kind string) string {
	english, found := absenceEnglish[kind]
	if !found {
		english = kind
	}

	return w.words.Text("absence."+kind, english)
}
