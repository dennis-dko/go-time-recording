package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

// NewTimerService creates new instance.
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
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	if description != nil && model.TooLong(*description, model.MaxDescriptionLength) {
		return nil, apperror.InvalidFields("description")
	}

	timer := &model.RunningTimer{
		UserID:      userID,
		ProjectID:   projectID,
		Description: description,
		// Truncated to the second. Nothing here needs more, and a stored
		// nanosecond would come back from the three engines with three different
		// amounts of precision.
		StartedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := s.timers.Start(ctx, timer); err != nil {
		return nil, err
	}

	return timer, nil
}

// Discard throws the clock away without recording anything.
func (s *TimerService) Discard(ctx context.Context, userID uint) error {
	return s.timers.Clear(ctx, userID)
}

// Stop turns the running clock into a time entry.
//
// zone is the user's own, and decides which calendar day the entry lands on: the
// day the clock was started, not the day it was stopped.
func (s *TimerService) Stop(
	ctx context.Context,
	userID uint,
	zone *time.Location,
) (*command.CreateTimesheetCommandResult, error) {
	timer, err := s.timers.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if timer == nil {
		return nil, apperror.Conflictf("no timer is running").WithCode("noTimerRunning")
	}

	now := time.Now().UTC()

	// Refused rather than rounded up to the floor. A clock started and stopped by
	// accident should leave no record, and the way to get rid of it is to discard
	// it - which the message says.
	if timer.TimerTooShort(now) {
		return nil, apperror.Invalidf(
			"the timer has been running for less than the smallest bookable duration; " +
				"discard it instead").
			WithCode("timerTooShort")
	}

	// Almost always a clock somebody forgot rather than a shift somebody worked,
	// and a single entry cannot hold more than a day anyway. The measured duration
	// is in the message so it can be booked by hand, split how it actually
	// happened - which is a decision only the person who was there can make.
	if timer.TimerTooLong(now) {
		return nil, apperror.Invalidf(
			"the timer has been running for %.1f hours, which is more than one entry can hold; "+
				"book it by hand and discard the timer", timer.HoursElapsed(now)).
			WithCode("timerTooLong", timer.HoursElapsed(now))
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
		DurationHours: timer.HoursElapsed(now),
		Description:   timer.Description,
	})
	if err != nil {
		// The clock stays, so the time is not lost to a refusal the user can act
		// on - a daily cap reached, a project archived while the clock ran. They
		// can change what it points at and stop it again.
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
