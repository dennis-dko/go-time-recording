package service_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// timerFixture is the booking fixture with a clock beside it. The repository is
// kept so a case can put a clock in the past directly, which is the only way to
// test a forgotten one without waiting a day for it.
type timerFixture struct {
	*fixture

	timers *memory.TimerRepository
	clock  *service.TimerService
}

func newTimerFixture(t *testing.T) *timerFixture {
	t.Helper()

	f := newFixture(t)
	timers := memory.NewTimerRepository()

	return &timerFixture{fixture: f, timers: timers, clock: service.NewTimerService(timers, f.timesheets)}
}

// runSince puts a clock on the fixture's project that started at the given moment.
func (f *timerFixture) runSince(t *testing.T, startedAt time.Time) {
	t.Helper()

	projectID := f.projectID

	if err := f.timers.Start(context.Background(), &model.RunningTimer{
		UserID: f.userID, ProjectID: &projectID, StartedAt: startedAt.UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("put a clock in place: %v", err)
	}
}

func (f *timerFixture) running(t *testing.T) *model.RunningTimer {
	t.Helper()

	timer, err := f.timers.Get(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read the clock: %v", err)
	}

	return timer
}

func requireCode(t *testing.T, err error, want string) {
	t.Helper()

	detail, ok := apperror.Detail(err)
	if !ok {
		t.Fatalf("expected a refusal coded %q, got %v", want, err)
	}

	if detail.Code != want {
		t.Fatalf("expected the code %q, got %q (%v)", want, detail.Code, err)
	}
}

// Somebody who started work at ten past nine and pressed the button at half past
// can say so. Until now the only way to correct it was to book by hand and throw
// the clock away, which also threw away the start it knew.
func TestTheStartOfARunningClockCanBeMovedBack(t *testing.T) {
	f := newTimerFixture(t)
	ctx := context.Background()

	f.runSince(t, time.Now().Add(-5*time.Minute))

	earlier := time.Now().Add(-2 * time.Hour)

	timer, err := f.clock.AdjustStart(ctx, f.userID, earlier)
	if err != nil {
		t.Fatalf("moving the start back was refused: %v", err)
	}

	want := earlier.UTC().Truncate(time.Second)

	if timer == nil || !timer.StartedAt.Equal(want) {
		t.Fatalf("the clock answered with %+v, want it started at %v", timer, want)
	}

	if stored := f.running(t); !stored.StartedAt.Equal(want) {
		t.Errorf("the stored clock started at %v, want %v", stored.StartedAt, want)
	}

	// The project the clock was started with is not lost on the way.
	if stored := f.running(t); stored.ProjectID == nil || *stored.ProjectID != f.projectID {
		t.Errorf("moving the start lost the project: %+v", stored)
	}

	booked, err := f.clock.Stop(ctx, f.userID, time.UTC, nil)
	if err != nil {
		t.Fatalf("stopping the corrected clock: %v", err)
	}

	if hours := booked.Result.DurationHours; hours < 1.99 || hours > 2.01 {
		t.Errorf("the booking holds %.4f h, want the two hours since the corrected start", hours)
	}
}

// A start in the future would be a clock that counts down.
func TestAClockCannotBeMadeToStartInTheFuture(t *testing.T) {
	f := newTimerFixture(t)

	started := time.Now().Add(-time.Hour)
	f.runSince(t, started)

	_, err := f.clock.AdjustStart(context.Background(), f.userID, time.Now().Add(time.Hour))

	requireKind(t, err, apperror.KindInvalid)
	requireCode(t, err, "timerStartInFuture")

	if stored := f.running(t); !stored.StartedAt.Equal(started.UTC().Truncate(time.Second)) {
		t.Errorf("the refused correction moved the clock to %v", stored.StartedAt)
	}
}

// There is nothing to correct when nothing runs, and saying so is the same answer
// stopping a clock that is not running gets.
func TestMovingTheStartNeedsARunningClock(t *testing.T) {
	f := newTimerFixture(t)

	_, err := f.clock.AdjustStart(context.Background(), f.userID, time.Now().Add(-time.Hour))

	requireKind(t, err, apperror.KindConflict)
	requireCode(t, err, "noTimerRunning")
}

// The case the feature exists for. A clock left running over night cannot become
// one entry, and the refusal used to send its owner off to book by hand. With the
// time the work ended it books exactly that, on the day the clock was started.
func TestAForgottenClockIsBookedUpToTheTimeItEnded(t *testing.T) {
	f := newTimerFixture(t)
	ctx := context.Background()

	started := time.Now().Add(-30 * time.Hour).UTC().Truncate(time.Second)
	f.runSince(t, started)

	// Refused without an end, as before: thirty hours do not fit in an entry.
	_, err := f.clock.Stop(ctx, f.userID, time.UTC, nil)
	requireCode(t, err, "timerTooLong")

	ended := started.Add(8*time.Hour + 30*time.Minute)

	booked, err := f.clock.Stop(ctx, f.userID, time.UTC, &ended)
	if err != nil {
		t.Fatalf("stopping at the time the work ended was refused: %v", err)
	}

	if math.Abs(booked.Result.DurationHours-8.5) > 1e-9 {
		t.Errorf("the booking holds %v h, want 8.5", booked.Result.DurationHours)
	}

	if want := model.CalendarDay(started); !booked.Result.Date.Equal(want) {
		t.Errorf("the booking is on %v, want the day the clock started, %v", booked.Result.Date, want)
	}

	if f.running(t) != nil {
		t.Error("the clock survived being booked")
	}
}

// An end before the start is a typing mistake, and it is named as one - "discard it
// instead", which the too-short refusal says, would be the wrong advice.
func TestAClockCannotStopBeforeItStarted(t *testing.T) {
	f := newTimerFixture(t)

	started := time.Now().Add(-3 * time.Hour)
	f.runSince(t, started)

	before := started.Add(-time.Minute)

	_, err := f.clock.Stop(context.Background(), f.userID, time.UTC, &before)

	requireKind(t, err, apperror.KindInvalid)
	requireCode(t, err, "timerEndBeforeStart")

	if f.running(t) == nil {
		t.Error("the refused stop threw the clock away")
	}
}

// Work that has not ended yet cannot be booked as ended.
func TestAClockCannotStopInTheFuture(t *testing.T) {
	f := newTimerFixture(t)

	f.runSince(t, time.Now().Add(-time.Hour))

	later := time.Now().Add(10 * time.Minute)

	_, err := f.clock.Stop(context.Background(), f.userID, time.UTC, &later)

	requireKind(t, err, apperror.KindInvalid)
	requireCode(t, err, "timerEndInFuture")

	if f.running(t) == nil {
		t.Error("the refused stop threw the clock away")
	}
}

// An end time is not a way past the ceiling: it is measured like any other stop.
func TestAnEndTimeStillMeetsTheCeilingOfOneEntry(t *testing.T) {
	f := newTimerFixture(t)

	started := time.Now().Add(-40 * time.Hour)
	f.runSince(t, started)

	ended := started.Add(25 * time.Hour)

	_, err := f.clock.Stop(context.Background(), f.userID, time.UTC, &ended)

	requireCode(t, err, "timerTooLong")
}

// Switching books what was measured and starts the next clock at the same instant,
// so not a second falls between the two - which is what three separate presses
// could not promise.
func TestSwitchingBooksTheClockAndStartsTheNextWithoutAGap(t *testing.T) {
	f := newTimerFixture(t)
	ctx := context.Background()

	next, err := f.projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: "Support", StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), OwnerID: &f.userID,
	})
	if err != nil {
		t.Fatalf("seed the second project: %v", err)
	}

	f.runSince(t, time.Now().Add(-2*time.Hour))

	nextID := next.Result.ID
	description := "Customer call"

	switched, err := f.clock.Switch(ctx, f.userID, time.UTC, &nextID, &description)
	if err != nil {
		t.Fatalf("switching was refused: %v", err)
	}

	if switched == nil || switched.Booked == nil || switched.Timer == nil {
		t.Fatalf("the switch answered with %+v, want a booking and a running clock", switched)
	}

	booked := switched.Booked.Result
	if booked.ProjectID == nil || *booked.ProjectID != f.projectID {
		t.Errorf("the booking went to %v, want the project the clock ran on, %d", booked.ProjectID, f.projectID)
	}

	if booked.DurationHours < 1.99 || booked.DurationHours > 2.01 {
		t.Errorf("the booking holds %.4f h, want the two hours measured", booked.DurationHours)
	}

	timer := f.running(t)
	if timer == nil || timer.ProjectID == nil || *timer.ProjectID != nextID {
		t.Fatalf("the clock now running is %+v, want one on project %d", timer, nextID)
	}

	if timer.Description == nil || *timer.Description != description {
		t.Errorf("the new clock carries %v, want the description it was given", timer.Description)
	}

	// The second clock starts where the first one's measurement ended.
	firstStart := time.Now().Add(-2 * time.Hour)
	measuredUntil := firstStart.Add(time.Duration(booked.DurationHours * float64(time.Hour)))

	if gap := timer.StartedAt.Sub(measuredUntil); gap < -2*time.Second || gap > 2*time.Second {
		t.Errorf("the new clock starts %v away from the end of the booked one", gap)
	}
}

// A switch that cannot book leaves everything as it was: the clock keeps running
// and no second one starts. Booking and then failing to start, or starting and
// then failing to book, would each lose somebody time.
func TestASwitchThatCannotBookKeepsTheClockAndStartsNothing(t *testing.T) {
	f := newTimerFixture(t)
	ctx := context.Background()

	started := time.Now().Add(-time.Hour)
	f.runSince(t, started)

	// The day the clock books on is already full.
	f.book(t, model.CalendarDay(started.UTC()), maxDailyHours)

	_, err := f.clock.Switch(ctx, f.userID, time.UTC, nil, nil)

	requireCode(t, err, "overDailyLimit")

	timer := f.running(t)
	if timer == nil || timer.ProjectID == nil || *timer.ProjectID != f.projectID ||
		!timer.StartedAt.Equal(started.UTC().Truncate(time.Second)) {
		t.Errorf("the refused switch changed the clock to %+v", timer)
	}
}

// A switch to a project that would refuse the next booking is refused before
// anything is booked, so the measured time is not split for nothing.
func TestASwitchToAProjectThatTakesNoTimeBooksNothing(t *testing.T) {
	f := newTimerFixture(t)
	ctx := context.Background()

	closed, err := f.projects.CreateProject(ctx, command.CreateProjectCommand{
		Name: "Done", StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), OwnerID: &f.userID,
		Status: model.ProjectStatusCompleted,
	})
	if err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	f.runSince(t, time.Now().Add(-time.Hour))

	closedID := closed.Result.ID

	_, err = f.clock.Switch(ctx, f.userID, time.UTC, &closedID, nil)

	requireCode(t, err, "projectClosedForBooking")

	entries, err := f.timesheetRepo.GetByFilter(ctx, repository.TimesheetFilter{UserID: f.userID})
	if err != nil {
		t.Fatalf("read the entries: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("the refused switch booked %d entr(y/ies)", len(entries))
	}

	if timer := f.running(t); timer == nil || *timer.ProjectID != f.projectID {
		t.Errorf("the refused switch changed the clock to %+v", timer)
	}
}
