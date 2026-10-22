package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// reminderFixture is the booking fixture with absences, the holidays of a state
// and a clock it can set.
type reminderFixture struct {
	*fixture

	absences *memory.AbsenceRepository
	region   string
	today    time.Time
}

func newReminderFixture(t *testing.T) *reminderFixture {
	t.Helper()

	return &reminderFixture{fixture: newFixture(t), absences: memory.NewAbsenceRepository()}
}

func (f *reminderFixture) unbooked(t *testing.T) *time.Time {
	t.Helper()

	overtime := service.NewOvertimeService(f.timesheetRepo, f.userRepo, memory.NewWorkScheduleRepository(),
		f.absences, func(context.Context) (string, error) { return f.region, nil },
		func(context.Context, *model.User) time.Time { return f.today })

	notices, err := overtime.Notices(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("notices: %v", err)
	}

	return notices.UnbookedDay
}

func (f *reminderFixture) away(t *testing.T, on time.Time, kind string) {
	t.Helper()

	if err := f.absences.Save(context.Background(),
		[]model.Absence{{UserID: f.userID, Day: on, Kind: kind}}); err != nil {
		t.Fatalf("record an absence: %v", err)
	}
}

// The reminder is about the last day somebody was expected to work, and only that
// one: a weekend, a day of leave, a day off against overtime and a public holiday
// are passed over on the way back to it, half a day of leave is not, and an earlier
// gap is the business of the day it was. Nothing is said about days before the
// first booking, which belong to a time before the account was used.
func TestTheLastWorkingDayWithNothingBookedIsMentioned(t *testing.T) {
	// July 2026: the 17th is a Friday and the 20th a Monday.
	cases := []struct {
		name  string
		setup func(t *testing.T, f *reminderFixture)
		want  *time.Time
	}{
		{"yesterday was booked", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
			f.book(t, day(20), 8)
		}, nil},
		{"yesterday was not", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
		}, ptr(day(20))},
		{"a Monday looks back past the weekend", func(t *testing.T, f *reminderFixture) {
			f.today = day(20)
			f.book(t, day(13), 8)
		}, ptr(day(17))},
		{"a day of leave is passed over", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
			f.away(t, day(20), model.AbsenceVacation)
		}, ptr(day(17))},
		{"a day off against overtime is passed over", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
			f.book(t, day(17), 8)
			f.away(t, day(20), model.AbsenceFlexDay)
		}, nil},
		{"half a day of leave still asks for the other half", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
			f.away(t, day(20), model.AbsenceHalfVacation)
		}, ptr(day(20))},
		{"an earlier gap is not the last working day's", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)
			f.book(t, day(20), 8)
		}, nil},
		{"nothing before the first booking", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(21), 8)
		}, nil},
		{"switched off", func(t *testing.T, f *reminderFixture) {
			f.today = day(21)
			f.book(t, day(13), 8)

			if err := f.userRepo.SetReminders(context.Background(), f.userID, true, nil); err != nil {
				t.Fatalf("switch the reminder off: %v", err)
			}
		}, nil},
		{"a public holiday is passed over", func(t *testing.T, f *reminderFixture) {
			// Christmas Day 2026 is a Friday; the 24th is a working day.
			f.region = "BY"
			f.today = time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC)
			f.book(t, day(13), 8)
		}, ptr(time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReminderFixture(t)
			tc.setup(t, f)

			got := f.unbooked(t)

			switch {
			case tc.want == nil && got != nil:
				t.Errorf("mentions %s, want nothing", got.Format(time.DateOnly))
			case tc.want != nil && (got == nil || !got.Equal(*tc.want)):
				t.Errorf("mentions %v, want %s", got, tc.want.Format(time.DateOnly))
			}
		})
	}
}

// Both reminder choices are kept together, and the stopwatch's hours lie between
// never and a day: past a day the stopwatch already says it holds more than one
// entry can.
func TestTheReminderChoicesAreKeptAndBounded(t *testing.T) {
	f := newFixture(t)

	if err := f.sessions.SetReminders(context.Background(), f.userID, false, 6); err != nil {
		t.Fatalf("keep the choices: %v", err)
	}

	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read the account: %v", err)
	}

	if !user.SkipUnbookedReminder || user.EffectiveTimerAlertHours() != 6 {
		t.Errorf("the account keeps skip=%v and %d hours, want the reminder off and 6 hours",
			user.SkipUnbookedReminder, user.EffectiveTimerAlertHours())
	}

	for _, hours := range []int{-1, model.MaxTimerAlertHours + 1} {
		err := f.sessions.SetReminders(context.Background(), f.userID, true, hours)

		if detail, ok := apperror.Detail(err); !ok || !slices.Contains(detail.Fields, "timerAlertHours") {
			t.Errorf("%d hours answered %v, want timerAlertHours refused", hours, err)
		}
	}
}
