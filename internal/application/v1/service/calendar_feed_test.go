package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// feedFixture is the planned fixture with somebody's calendar feed beside it.
type feedFixture struct {
	*plannedFixture

	feeds *service.CalendarFeedService
}

func newFeedFixture(t *testing.T) *feedFixture {
	t.Helper()

	f := newPlannedFixture(t)

	// Past the initial password, as anybody who has opened the application to copy
	// an address into their calendar is: an account still waiting to choose one is
	// refused by the feed, which is a case of its own below.
	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read the account: %v", err)
	}

	user.MustChangePassword = false
	if _, err := f.userRepo.Update(context.Background(), user); err != nil {
		t.Fatalf("choose a password: %v", err)
	}

	clock := service.Today(func(context.Context, *model.User) time.Time { return f.today })
	name := service.CalendarName(func(_ context.Context, language string) string {
		return "Zeiterfassung (" + language + ")"
	})

	return &feedFixture{
		plannedFixture: f,
		feeds: service.NewCalendarFeedService(memory.NewCalendarFeedRepository(), f.userRepo,
			f.timesheetRepo, f.absenceRepo, f.projectRepo, f.auth, clock, name),
	}
}

// subscribe gives the fixture's person a feed and returns its key.
func (f *feedFixture) subscribe(t *testing.T) string {
	t.Helper()

	issued, err := f.feeds.Create(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("create the feed: %v", err)
	}

	return issued.Key
}

// read reads the feed the way a calendar program asks for it, and unfolds it.
func (f *feedFixture) read(t *testing.T, key string) string {
	t.Helper()

	written, err := f.feeds.Read(context.Background(), key+".ics")
	if err != nil {
		t.Fatalf("read the feed: %v", err)
	}

	return strings.ReplaceAll(string(written), "\r\n ", "")
}

// unread asks for a feed that must not answer.
func (f *feedFixture) unread(t *testing.T, key, why string) {
	t.Helper()

	_, err := f.feeds.Read(context.Background(), key)
	if apperror.KindOf(err) != apperror.KindNotFound {
		t.Errorf("%s: the feed answered %v, want not found", why, err)
	}
}

// The feed has a day for every day somebody booked on, saying how much, with what
// they booked written under it - and a day for every day they are away, saying why.
func TestACalendarFeedHasTheBookedDaysAndTheAbsences(t *testing.T) {
	f := newFeedFixture(t)

	f.book(t, day(14), 2.5)
	f.book(t, day(14), 5)
	f.takeLeave(t, day(20), day(20), model.AbsenceVacation)

	feed := f.read(t, f.subscribe(t))

	for _, want := range []string{
		"X-WR-CALNAME:Zeiterfassung (en)",
		"DTSTART;VALUE=DATE:20260714\r\nDTEND;VALUE=DATE:20260715\r\nSUMMARY:7.50 h booked\r\n" +
			`DESCRIPTION:2.50 h · Website\n5.00 h · Website`,
		"DTSTART;VALUE=DATE:20260720\r\nDTEND;VALUE=DATE:20260721\r\nSUMMARY:Leave\r\n",
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed has no %q in:\n%s", want, feed)
		}
	}

	if days := strings.Count(feed, "BEGIN:VEVENT"); days != 2 {
		t.Errorf("the feed has %d days, want the booked one and the one away", days)
	}
}

// A feed is read by a calendar program, which has no language to ask in, so it is
// written in the one its owner reads the application in.
func TestACalendarFeedIsWrittenInItsOwnersLanguage(t *testing.T) {
	f := newFeedFixture(t)

	if err := f.sessions.SetLanguage(context.Background(), f.userID, model.LanguageGerman); err != nil {
		t.Fatalf("choose German: %v", err)
	}

	f.book(t, day(14), 7.5)
	f.takeLeave(t, day(20), day(20), model.AbsenceVacation)

	feed := f.read(t, f.subscribe(t))

	// The comma is escaped, as the format asks of every comma in text; a calendar
	// program shows it as the comma it is.
	for _, want := range []string{
		"X-WR-CALNAME:Zeiterfassung (de)",
		`SUMMARY:7\,50 Std. gebucht` + "\r\n" + `DESCRIPTION:7\,50 Std. · Website` + "\r\n",
		"SUMMARY:Urlaub\r\n",
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("the feed has no %q in:\n%s", want, feed)
		}
	}
}

// Only a hash of the key is kept, and the start of it to recognise it by. Making a
// new one takes the old one back, and so does removing it.
func TestAKeyThatWasReplacedOrRemovedReadsNothing(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()

	first := f.subscribe(t)
	second := f.subscribe(t)

	if !strings.HasPrefix(second, service.CalendarFeedKeyPrefix) {
		t.Errorf("the key %q does not start with %q", second, service.CalendarFeedKeyPrefix)
	}

	kept, err := f.feeds.Feed(ctx, f.userID)
	if err != nil || kept == nil {
		t.Fatalf("the feed as kept: %v, %v", kept, err)
	}

	if strings.Contains(kept.KeyHash, second[len(service.CalendarFeedKeyPrefix):]) {
		t.Errorf("the key is kept as it was given: %q", kept.KeyHash)
	}

	if want := second[:len(service.CalendarFeedKeyPrefix)+6]; kept.Prefix != want {
		t.Errorf("the feed is recognised by %q, want %q", kept.Prefix, want)
	}

	f.unread(t, first, "the key a new one replaced")
	f.read(t, second)

	if err := f.feeds.Remove(ctx, f.userID); err != nil {
		t.Fatalf("remove the feed: %v", err)
	}

	f.unread(t, second, "the key of a removed feed")

	if gone, err := f.feeds.Feed(ctx, f.userID); err != nil || gone != nil {
		t.Errorf("after removing it the feed reads %+v, %v, want none", gone, err)
	}

	requireKind(t, f.feeds.Remove(ctx, f.userID), apperror.KindNotFound)
}

// A key is never more than its owner: when their role stops letting them read their
// own entries, or their password has been reset and waits to be chosen, the feed
// reads nothing - as an API token does.
func TestAFeedReadsNothingTheOwnerCouldNotReadThemselves(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()

	key := f.subscribe(t)
	f.read(t, key)

	f.unread(t, "gtrcal_somethingnobodywasgiven", "a key nobody was given")
	f.unread(t, strings.TrimPrefix(key, service.CalendarFeedKeyPrefix), "the key without its prefix")

	role, err := f.roles.CreateRole(ctx, "auditor", "", []string{model.PermUserRead})
	if err != nil {
		t.Fatalf("create a role: %v", err)
	}

	user, err := f.userRepo.GetByID(ctx, f.userID)
	if err != nil {
		t.Fatalf("read the account: %v", err)
	}

	worker := user.RoleID

	// Each change is undone before the next, and the feed read again in between, so
	// that each refusal is the one change's and not the one before it still in place.
	change := func(what string, apply func(*model.User)) {
		t.Helper()

		apply(user)

		if _, err := f.userRepo.Update(ctx, user); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	change("give the account the role", func(u *model.User) { u.RoleID = role.ID })
	f.unread(t, key, "an owner who may not read their own entries")

	change("give the role back", func(u *model.User) { u.RoleID = worker })
	f.read(t, key)

	change("reset the password", func(u *model.User) { u.MustChangePassword = true })
	f.unread(t, key, "an owner whose password waits to be chosen")
}

// Somebody's feed is their own days: not another person's hours, not the name of a
// project they may not see, and not what lies more than a year away.
func TestAFeedHoldsOnlyTheOwnersOwnYear(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()

	colleague, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Colleague", Email: "colleague@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create a colleague: %v", err)
	}

	theirs := colleague.Result.ID

	secret, err := f.projectRepo.Save(ctx, &model.Project{
		Name: "Secret project", StartDate: day(1), Status: model.ProjectStatusActive, OwnerID: &theirs,
	})
	if err != nil {
		t.Fatalf("give the colleague a project: %v", err)
	}

	for _, entry := range []*model.Timesheet{
		// The colleague's own hours.
		{UserID: theirs, ProjectID: &secret.ID, Date: day(15), DurationHours: 4},
		// The owner's, on the colleague's project - from the days projects were shared.
		{UserID: f.userID, ProjectID: &secret.ID, Date: day(16), DurationHours: 3},
		// The owner's, a year and a day before today, and a year after it.
		{UserID: f.userID, Date: time.Date(2025, time.July, 30, 0, 0, 0, 0, time.UTC), DurationHours: 1},
		{UserID: f.userID, Date: time.Date(2025, time.July, 31, 0, 0, 0, 0, time.UTC), DurationHours: 2},
		{UserID: f.userID, Date: time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC), DurationHours: 1},
	} {
		if _, err := f.timesheetRepo.Save(ctx, entry); err != nil {
			t.Fatalf("leave an entry: %v", err)
		}
	}

	feed := f.read(t, f.subscribe(t))

	if strings.Contains(feed, "20260715") {
		t.Errorf("the colleague's day is in the feed:\n%s", feed)
	}

	if strings.Contains(feed, "Secret project") {
		t.Errorf("a project its owner cannot see is named:\n%s", feed)
	}

	if !strings.Contains(feed, "DTSTART;VALUE=DATE:20260716\r\nDTEND;VALUE=DATE:20260717\r\nSUMMARY:3.00 h booked\r\nDESCRIPTION:3.00 h\r\n") {
		t.Errorf("the day booked on the unseen project is not in the feed without its name:\n%s", feed)
	}

	if !strings.Contains(feed, "DTSTART;VALUE=DATE:20250731") {
		t.Errorf("the day a year before today is missing:\n%s", feed)
	}

	for _, outside := range []string{"20250730", "20270801"} {
		if strings.Contains(feed, "DTSTART;VALUE=DATE:"+outside) {
			t.Errorf("%s, more than a year from today, is in the feed", outside)
		}
	}
}
