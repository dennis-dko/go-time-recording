package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// hoursOn lists what is booked on one day, in the order it was booked.
func (f *fixture) hoursOn(t *testing.T, on time.Time) []float64 {
	t.Helper()

	entries, err := f.timesheetRepo.GetByFilter(context.Background(), repository.TimesheetFilter{
		UserID: f.userID, StartDate: &on, EndDate: &on,
	})
	if err != nil {
		t.Fatalf("read %s: %v", on.Format(time.DateOnly), err)
	}

	slices.SortFunc(entries, func(a, b *model.Timesheet) int { return int(a.ID) - int(b.ID) })

	hours := make([]float64, 0, len(entries))
	for _, entry := range entries {
		hours = append(hours, entry.DurationHours)
	}

	return hours
}

// A week is copied day for day into the days of the next that have nothing on
// them, and a day already booked is left exactly as it was.
func TestCopyingAWeekFillsOnlyTheDaysWithNothingOnThem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// 6 July 2026 is a Monday.
	f.book(t, day(6), 4)

	if _, err := f.timesheets.CreateTimesheet(ctx, command.CreateTimesheetCommand{
		UserID: f.userID, Date: day(6), DurationHours: 2,
	}); err != nil {
		t.Fatalf("book without a project: %v", err)
	}

	f.book(t, day(7), 3)
	f.book(t, day(14), 1) // the Tuesday of the week being filled, already booked

	copied, err := f.timesheets.CopyWeek(ctx, f.userID, day(6), day(13))
	if err != nil {
		t.Fatalf("copy: %v", err)
	}

	if copied.Copied != 2 || copied.Hours != 6 || copied.Kept != 1 || copied.Closed != 0 {
		t.Errorf("the copy reads %+v, want two entries of six hours and one day kept", copied)
	}

	if monday := f.hoursOn(t, day(13)); !slices.Equal(monday, []float64{4, 2}) {
		t.Errorf("the Monday holds %v, want last Monday's 4 and 2", monday)
	}

	if tuesday := f.hoursOn(t, day(14)); !slices.Equal(tuesday, []float64{1}) {
		t.Errorf("the Tuesday holds %v, want the hour that was already there and nothing added", tuesday)
	}
}

// An entry on a project that no longer takes bookings is left out, and the rest
// of the week is copied regardless.
func TestCopyingAWeekLeavesOutAProjectThatHasClosed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.book(t, day(6), 4)

	if _, err := f.timesheets.CreateTimesheet(ctx, command.CreateTimesheetCommand{
		UserID: f.userID, Date: day(7), DurationHours: 2,
	}); err != nil {
		t.Fatalf("book without a project: %v", err)
	}

	completed := model.ProjectStatusCompleted
	if _, err := f.projects.UpdateProject(ctx,
		command.UpdateProjectCommand{ID: f.projectID, Status: &completed}); err != nil {
		t.Fatalf("complete the project: %v", err)
	}

	copied, err := f.timesheets.CopyWeek(ctx, f.userID, day(6), day(13))
	if err != nil {
		t.Fatalf("copy: %v", err)
	}

	if copied.Copied != 1 || copied.Closed != 1 {
		t.Errorf("the copy reads %+v, want the entry without a project copied and the other left out", copied)
	}

	if monday := f.hoursOn(t, day(13)); len(monday) != 0 {
		t.Errorf("the Monday holds %v, want nothing - its entry is on a finished project", monday)
	}
}

// A copy the daily cap would refuse on one day writes nothing on any day.
func TestCopyingAWeekIsAllOrNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.book(t, day(6), 3)
	f.book(t, day(7), 6)
	f.book(t, day(7), 3)

	// Lowered after the week was booked, so the Tuesday no longer fits.
	target, ceiling := 6.0, 8.0

	if _, err := f.users.UpdateWorkingTimes(ctx, command.UpdateWorkingTimesCommand{
		ID: f.userID, DailyTargetHours: &target, MaxDailyHours: &ceiling,
	}); err != nil {
		t.Fatalf("lower the ceiling: %v", err)
	}

	_, err := f.timesheets.CopyWeek(ctx, f.userID, day(6), day(13))
	requireCode(t, err, "overDailyLimit")

	if monday := f.hoursOn(t, day(13)); len(monday) != 0 {
		t.Errorf("the Monday holds %v after a refused copy, want nothing", monday)
	}
}

// Weeks are named by their Mondays, a week is not copied onto itself, and an
// empty week is refused by name rather than answered with nothing done.
func TestCopyingAWeekNeedsTwoWeeksAndSomethingInTheFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.timesheets.CopyWeek(ctx, f.userID, day(7), day(13))
	requireKind(t, err, apperror.KindInvalid)

	_, err = f.timesheets.CopyWeek(ctx, f.userID, day(13), day(13))
	requireKind(t, err, apperror.KindInvalid)

	_, err = f.timesheets.CopyWeek(ctx, f.userID, day(6), day(13))
	requireCode(t, err, "nothingToCopy")
}
