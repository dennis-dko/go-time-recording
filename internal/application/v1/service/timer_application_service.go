package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// TimerService starts and stops the clock, and turns a stopped one into a time
// entry.
//
// The entry is created through the timesheet service rather than written
// straight to the repository, so a timer booking meets exactly the rules a typed
// booking meets: the daily cap, the project having to exist and be active and be
// visible, the description length. A second path into the same table that
// enforced its own subset of those is how the two drift apart.
type TimerService struct {
	timers     repository.TimerRepository
	timesheets *TimesheetApplicationService
}

// NewTimerService is given the timesheet service rather than its repository, so
// a stopped clock is booked under every rule a typed entry is.
func NewTimerService(
	timers repository.TimerRepository,
	timesheets *TimesheetApplicationService,
) *TimerService {
	return &TimerService{timers: timers, timesheets: timesheets}
}

// Running returns the user's clock, or nil when none is running.
func (s *TimerService) Running(ctx context.Context, userID uint) (*model.RunningTimer, error) {
	return s.timers.Get(ctx, userID)
}

// Start begins timing, replacing any clock the user already had running.
//
// Replacing rather than refusing: somebody who starts a second time has changed
// their mind about what they are doing, and a refusal would leave them to stop the
// first one - producing an entry for work they were not doing.
func (s *TimerService) Start(
	ctx context.Context,
	userID uint,
	projectID *uint,
	description *string,
) (*model.RunningTimer, error) {
	if err := s.requireStartable(ctx, userID, projectID, description); err != nil {
		return nil, err
	}

	return s.startAt(ctx, userID, projectID, description, readClock())
}

// requireStartable is everything a clock is checked against before it starts.
//
// The project is checked here, at the start, and not only when the clock is
// stopped. Three reasons, and the middle one is why this was a bug rather than a
// preference:
//
// Told now, somebody can pick a different project before spending an afternoon
// measuring time against one that will not take it.
//
// The column is a foreign key. SQLite does not enforce those by default, so a
// projectId that does not exist was stored happily and left a clock that could
// never be stopped; PostgreSQL refuses the insert and the person gets a 500 for a
// mistyped number. Found by the integration suite's PostgreSQL leg, which is the
// only place the two behaviours differ visibly.
//
// And it is the same check the booking path makes, called rather than restated -
// so a project somebody may not see is "not found" here exactly as it is there.
func (s *TimerService) requireStartable(
	ctx context.Context,
	userID uint,
	projectID *uint,
	description *string,
) error {
	if userID == 0 {
		return apperror.InvalidFields("userId")
	}

	if description != nil && model.TooLong(*description, model.MaxDescriptionLength) {
		return apperror.InvalidFields("description")
	}

	if projectID != nil {
		return s.timesheets.requireUserAndProject(ctx, userID, *projectID)
	}

	return nil
}

func (s *TimerService) startAt(
	ctx context.Context,
	userID uint,
	projectID *uint,
	description *string,
	startedAt time.Time,
) (*model.RunningTimer, error) {
	timer := &model.RunningTimer{
		UserID:      userID,
		ProjectID:   projectID,
		Description: description,
		StartedAt:   startedAt,
	}

	if err := s.timers.Start(ctx, timer); err != nil {
		return nil, err
	}

	return timer, nil
}

// readClock is the moment a clock is read at, truncated to the second.
//
// Nothing here needs more, and a stored nanosecond would come back from the three
// engines with three different amounts of precision. Read once per operation and
// passed along, so a switch ends one clock and starts the next at the same second
// rather than at two readings a few microseconds apart.
func readClock() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// Discard throws the clock away without recording anything.
func (s *TimerService) Discard(ctx context.Context, userID uint) error {
	return s.timers.Clear(ctx, userID)
}

// AdjustStart moves the start of the running clock.
//
// For the clock somebody started late: at work since ten past nine, the button
// pressed at half past. Until this existed the only correction was to book by hand
// and discard the clock, which threw away the start it knew along with the mistake.
//
// Earlier or later, up to now: a start in the future would be a clock counting
// down. How far back is not bounded. A start more than a day ago makes a clock that
// Stop refuses without an end time, which is the state a forgotten clock is in
// anyway, with the same way out. Moving it across midnight moves the day the entry
// will land on, because that day is the one the clock started on.
func (s *TimerService) AdjustStart(
	ctx context.Context,
	userID uint,
	startedAt time.Time,
) (*model.RunningTimer, error) {
	timer, err := s.running(ctx, userID)
	if err != nil {
		return nil, err
	}

	startedAt = startedAt.UTC().Truncate(time.Second)

	if startedAt.After(readClock()) {
		return nil, apperror.Invalidf("a clock cannot start in the future").WithCode("timerStartInFuture")
	}

	// Moved rather than started again, so a stop that lands between the read
	// above and this write is not undone: a clock brought back after it was
	// booked would count the same time twice.
	if err := s.timers.MoveStart(ctx, userID, startedAt); err != nil {
		return nil, err
	}

	timer.StartedAt = startedAt

	return timer, nil
}

// SwitchResult is what a switch did: the entry it booked and the clock it started.
type SwitchResult struct {
	Booked *command.CreateTimesheetCommandResult
	Timer  *model.RunningTimer
}

// Switch books the running clock and starts the next one at the same second.
//
// Changing what one works on was three presses - stop, choose, start - with no
// clock running between them, and through the API a second start replaced the
// running clock without booking it at all. This books what was measured up to now
// and starts the next clock at exactly that second, so the two entries meet
// without a gap.
//
// Checked before anything is written: the next project is held to the rules a
// start is, and a booking that would be refused - a full day, a project closed
// while the clock ran - leaves the clock running and starts nothing. What cannot
// be checked in advance is the write of the new clock after the booking; should
// that fail, the entry exists and the error says so, which is where a stop
// followed by a failed start would have left things too.
func (s *TimerService) Switch(
	ctx context.Context,
	userID uint,
	zone *time.Location,
	projectID *uint,
	description *string,
) (*SwitchResult, error) {
	timer, err := s.running(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := s.requireStartable(ctx, userID, projectID, description); err != nil {
		return nil, err
	}

	at := readClock()

	booked, err := s.book(ctx, timer, zone, at)
	if err != nil {
		return nil, err
	}

	next, err := s.startAt(ctx, userID, projectID, description, at)
	if err != nil {
		return nil, err
	}

	return &SwitchResult{Booked: booked, Timer: next}, nil
}

// Stop turns the running clock into a time entry.
//
// zone is the user's own, and decides which calendar day the entry lands on: the
// day the clock was started, not the day it was stopped.
//
// endedAt is when the work ended, or nil for now. A clock somebody forgot is then
// booked up to the time they really stopped, where before it was refused and left
// to be typed in by hand. The end is held to the rules the clock is: after the
// start, not in the future, and no more than one entry can hold.
func (s *TimerService) Stop(
	ctx context.Context,
	userID uint,
	zone *time.Location,
	endedAt *time.Time,
) (*command.CreateTimesheetCommandResult, error) {
	timer, err := s.running(ctx, userID)
	if err != nil {
		return nil, err
	}

	end := readClock()

	if endedAt != nil {
		ended := endedAt.UTC().Truncate(time.Second)

		if ended.After(end) {
			return nil, apperror.Invalidf("a clock cannot be stopped at a time that has not come yet").
				WithCode("timerEndInFuture")
		}

		// Its own refusal rather than the too-short one, whose advice - discard
		// the clock - is wrong for what is almost always a mistyped time.
		if !ended.After(timer.StartedAt) {
			return nil, apperror.Invalidf("a clock cannot stop before it started").
				WithCode("timerEndBeforeStart")
		}

		end = ended
	}

	return s.book(ctx, timer, zone, end)
}

// running is the user's clock, or the refusal for having none.
func (s *TimerService) running(ctx context.Context, userID uint) (*model.RunningTimer, error) {
	timer, err := s.timers.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if timer == nil {
		return nil, apperror.Conflictf("no timer is running").WithCode("noTimerRunning")
	}

	return timer, nil
}

// book turns the clock into an entry measured up to end, and clears it.
func (s *TimerService) book(
	ctx context.Context,
	timer *model.RunningTimer,
	zone *time.Location,
	end time.Time,
) (*command.CreateTimesheetCommandResult, error) {
	userID := timer.UserID

	// Refused rather than rounded up to the floor. A clock started and stopped by
	// accident should leave no record, and the way to get rid of it is to discard
	// it - which the message says.
	if timer.TimerTooShort(end) {
		return nil, apperror.Invalidf(
			"the timer has been running for less than the smallest bookable duration; " +
				"discard it instead").
			WithCode("timerTooShort")
	}

	// Almost always a clock somebody forgot rather than a shift somebody worked,
	// and a single entry cannot hold more than a day anyway. The measured duration
	// is in the message, and the way out is to say when the work ended - which only
	// the person who was there knows.
	if timer.TimerTooLong(end) {
		return nil, apperror.Invalidf(
			"the clock measures %.1f hours, which is more than one entry can hold; "+
				"give the time the work ended, or discard it", timer.HoursElapsed(end)).
			WithCode("timerTooLong", timer.HoursElapsed(end))
	}

	// The command carries 0 for "no project", which is how a typed booking says
	// it too - a nil pointer here has to become that rather than an id of zero.
	var projectID uint
	if timer.ProjectID != nil {
		projectID = *timer.ProjectID
	}

	created, err := s.timesheets.CreateTimesheet(ctx, command.CreateTimesheetCommand{
		UserID:        userID,
		ProjectID:     projectID,
		Date:          timer.BookingDay(zone),
		DurationHours: timer.HoursElapsed(end),
		Description:   timer.Description,
	})
	if err != nil {
		// The clock stays, so the time is not lost to a refusal the user can act
		// on - a daily cap reached, a project completed or archived while the
		// clock ran. The way out is to make room and stop it again: reopen the
		// project, or shorten another entry of that day. Starting it again is
		// not one, because a start replaces the clock and its start with it.
		return nil, err
	}

	if err := s.timers.Clear(ctx, userID); err != nil {
		// The entry exists, which is what mattered. A clock that outlives its
		// booking would double-count if stopped again, so this is worth
		// reporting rather than swallowing.
		return nil, err
	}

	return created, nil
}
