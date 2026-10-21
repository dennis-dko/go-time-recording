package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// workFixture is the booking fixture with working days beside it.
type workFixture struct {
	*fixture

	work *service.WorkDayService
}

func newWorkFixture(t *testing.T) *workFixture {
	t.Helper()

	f := newFixture(t)

	return &workFixture{fixture: f, work: service.NewWorkDayService(memory.NewWorkDayRepository(), f.timesheetRepo)}
}

// clock is a time of day as minutes after midnight.
func clock(hour, minute int) int { return hour*60 + minute }

func (f *workFixture) record(t *testing.T, userID uint, on time.Time, start, end, breaks int) {
	t.Helper()

	if err := f.work.Record(context.Background(), model.WorkDay{
		UserID: userID, Day: on, StartMinute: start, EndMinute: end, BreakMinutes: breaks,
	}); err != nil {
		t.Fatalf("record %s: %v", on.Format(time.DateOnly), err)
	}
}

func (f *workFixture) days(t *testing.T, userID uint, from, to time.Time) []service.WorkDayRecord {
	t.Helper()

	days, err := f.work.Between(context.Background(), userID, from, to)
	if err != nil {
		t.Fatalf("read the days: %v", err)
	}

	return days
}

// A day stands with when it began and finished and how long its breaks were,
// beside the hours booked on it - and a day booked on with nothing recorded
// stands there too, as the gap it is.
func TestAWorkingDayStandsBesideTheHoursBookedOnIt(t *testing.T) {
	f := newWorkFixture(t)

	f.record(t, f.userID, day(14), clock(8, 0), clock(16, 30), 30)
	f.book(t, day(14), 2.5)
	f.book(t, day(14), 5)
	f.book(t, day(15), 3)

	days := f.days(t, f.userID, day(13), day(19))
	if len(days) != 2 {
		t.Fatalf("the week reads %d days, want the one recorded and the one only booked: %+v", len(days), days)
	}

	if recorded := days[0]; !recorded.Day.Equal(day(14)) || !recorded.Recorded ||
		recorded.WorkedMinutes() != 480 || recorded.BookedHours != 7.5 {
		t.Errorf("the 14th reads %+v, want eight hours worked beside 7.5 booked", recorded)
	}

	if booked := days[1]; !booked.Day.Equal(day(15)) || booked.Recorded || booked.BookedHours != 3 {
		t.Errorf("the 15th reads %+v, want three hours booked and nothing recorded", booked)
	}
}

// A day that cannot be is refused, naming what is wrong with it.
func TestAWorkingDayThatCannotBeIsRefused(t *testing.T) {
	f := newWorkFixture(t)

	for name, tc := range map[string]struct {
		day        model.WorkDay
		wrongField string
	}{
		"finished before it began":  {model.WorkDay{Day: day(14), StartMinute: clock(9, 0), EndMinute: clock(8, 0)}, "end"},
		"finished as it began":      {model.WorkDay{Day: day(14), StartMinute: clock(9, 0), EndMinute: clock(9, 0)}, "end"},
		"began before midnight":     {model.WorkDay{Day: day(14), StartMinute: -1, EndMinute: clock(8, 0)}, "start"},
		"finished after the day":    {model.WorkDay{Day: day(14), StartMinute: clock(8, 0), EndMinute: model.MinutesPerDay + 1}, "end"},
		"a negative break":          {model.WorkDay{Day: day(14), StartMinute: clock(8, 0), EndMinute: clock(9, 0), BreakMinutes: -5}, "breakMinutes"},
		"breaks as long as the day": {model.WorkDay{Day: day(14), StartMinute: clock(8, 0), EndMinute: clock(9, 0), BreakMinutes: 60}, "breakMinutes"},
		"no day at all":             {model.WorkDay{StartMinute: clock(8, 0), EndMinute: clock(9, 0)}, "date"},
	} {
		t.Run(name, func(t *testing.T) {
			tc.day.UserID = f.userID

			err := f.work.Record(context.Background(), tc.day)

			detail, ok := apperror.Detail(err)
			if !ok || detail.Kind != apperror.KindInvalid || len(detail.Fields) != 1 || detail.Fields[0] != tc.wrongField {
				t.Errorf("answered %v, want %q named", err, tc.wrongField)
			}
		})
	}

	if days := f.days(t, f.userID, day(1), day(31)); len(days) != 0 {
		t.Errorf("a refused day was kept: %+v", days)
	}
}

// Recording a day again replaces what it said, a day that ends at midnight is a
// day, and taking a day back leaves nothing of it.
func TestRecordingADayAgainReplacesItAndTakingItBackRemovesIt(t *testing.T) {
	f := newWorkFixture(t)
	ctx := context.Background()

	f.record(t, f.userID, day(14), clock(8, 0), clock(16, 0), 30)
	f.record(t, f.userID, day(14), clock(14, 0), model.MinutesPerDay, 0)

	days := f.days(t, f.userID, day(14), day(14))
	if len(days) != 1 || days[0].StartMinute != clock(14, 0) || days[0].WorkedMinutes() != 600 {
		t.Fatalf("the day reads %+v, want the second record, ten hours to midnight", days)
	}

	if err := f.work.Remove(ctx, f.userID, day(14)); err != nil {
		t.Fatalf("take the day back: %v", err)
	}

	if days := f.days(t, f.userID, day(14), day(14)); len(days) != 0 {
		t.Errorf("a day taken back still reads %+v", days)
	}

	requireKind(t, f.work.Remove(ctx, f.userID, day(14)), apperror.KindNotFound)
}

// Somebody's days are their own: a colleague's record and a colleague's hours are
// not in them.
func TestAWorkingDayIsItsOwnersOwn(t *testing.T) {
	f := newWorkFixture(t)
	ctx := context.Background()

	colleague, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Colleague", Email: "colleague@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create a colleague: %v", err)
	}

	theirs := colleague.Result.ID

	f.record(t, theirs, day(16), clock(7, 0), clock(15, 0), 0)

	if _, err := f.timesheetRepo.Save(ctx, &model.Timesheet{UserID: theirs, Date: day(17), DurationHours: 4}); err != nil {
		t.Fatalf("book the colleague's hours: %v", err)
	}

	if days := f.days(t, f.userID, day(13), day(19)); len(days) != 0 {
		t.Errorf("the owner's week holds a colleague's days: %+v", days)
	}
}

// A range is bounded as every answer given a day at a time is.
func TestWorkingDaysAreReadForABoundedRange(t *testing.T) {
	f := newWorkFixture(t)

	_, err := f.work.Between(context.Background(), f.userID, day(1), day(1).AddDate(1, 2, 0))

	if detail, ok := apperror.Detail(err); !ok || detail.Code != "rangeTooLong" {
		t.Errorf("a range of fourteen months answered %v, want rangeTooLong", err)
	}
}

// What the Working Hours Act says about a day stands beside it, and the rest
// before the first day of a range is counted from the day before it, which the
// range itself does not hold: the first of a month after a late last of the month
// before is as short a night as any other.
func TestTheActStandsBesideTheDayAndLooksBackPastTheRange(t *testing.T) {
	f := newWorkFixture(t)

	f.record(t, f.userID, day(14), clock(9, 0), clock(23, 0), 60)
	f.record(t, f.userID, day(15), clock(7, 0), clock(15, 0), 30)
	f.book(t, day(16), 2)

	days := f.days(t, f.userID, day(15), day(21))
	if len(days) != 2 {
		t.Fatalf("the week reads %d days, want the one recorded and the one booked: %+v", len(days), days)
	}

	want := []model.WorkingTimeNotice{{Rule: model.NoticeShortRest, Minutes: 480}}
	if got := days[0].Notices; len(got) != 1 || got[0] != want[0] {
		t.Errorf("the 15th says %+v, want eight hours of rest since the 14th", got)
	}

	if got := days[1].Notices; len(got) != 0 {
		t.Errorf("a day only booked on says %+v, want nothing: no times were recorded for it", got)
	}
}
