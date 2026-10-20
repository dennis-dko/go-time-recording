package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WorkScheduleService keeps the weeks somebody plans their hours in.
//
// Each person's own and nobody else's, like the daily target it refines: what a
// week asks of somebody is a time figure, which belongs to whoever it is about.
type WorkScheduleService struct {
	schedules repository.WorkScheduleRepository
	users     repository.UserRepository
}

// NewWorkScheduleService needs the accounts as well, because the week before the
// first plan is made of somebody's own daily target, and a plan is held to their
// own daily maximum.
func NewWorkScheduleService(
	schedules repository.WorkScheduleRepository,
	users repository.UserRepository,
) *WorkScheduleService {
	return &WorkScheduleService{schedules: schedules, users: users}
}

// PlannedWeeks is somebody's plans, earliest first, and the week that applies
// before the first - their daily target from Monday to Friday.
type PlannedWeeks struct {
	Schedules []model.WorkSchedule
	Before    model.WeekHours
}

// Planned returns somebody's plans.
func (s *WorkScheduleService) Planned(ctx context.Context, userID uint) (*PlannedWeeks, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	schedules, err := s.schedules.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &PlannedWeeks{
		Schedules: schedules,
		Before:    model.DefaultWeek(user.EffectiveDailyTarget()),
	}, nil
}

// Plan records a week from a day on, replacing a plan that starts that same day.
//
// Every day is held to what a day can hold, and to the most this person may book
// in one - a day planned above that could never be met, which is the refusal the
// daily target already gets, under the same code.
func (s *WorkScheduleService) Plan(
	ctx context.Context,
	userID uint,
	validFrom time.Time,
	hours model.WeekHours,
) (*model.WorkSchedule, error) {
	var invalid []string

	if !model.OnTheCalendar(validFrom) {
		invalid = append(invalid, "validFrom")
	}

	if !hours.Valid() {
		invalid = append(invalid, "hours")
	}

	if len(invalid) > 0 {
		return nil, apperror.InvalidFields(invalid...)
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if most := user.MaxDailyHours; most > 0 {
		for _, planned := range hours {
			if planned > most {
				return nil, apperror.Invalidf("the daily target (%.2fh) cannot exceed the daily maximum (%.2fh)",
					planned, most).
					WithCode("targetOverMaximum", planned, most)
			}
		}
	}

	schedule := model.WorkSchedule{
		UserID:    userID,
		ValidFrom: model.CalendarDay(validFrom),
		Hours:     hours,
	}

	if err := s.schedules.Save(ctx, schedule); err != nil {
		return nil, err
	}

	return &schedule, nil
}

// Remove takes back the plan that starts on a day, handing its days back to the
// plan before it - or to the daily target, where there is none.
func (s *WorkScheduleService) Remove(ctx context.Context, userID uint, validFrom time.Time) error {
	found, err := s.schedules.Delete(ctx, userID, model.CalendarDay(validFrom))
	if err != nil {
		return err
	}

	if !found {
		return apperror.NotFound("schedule", validFrom.Format(time.DateOnly))
	}

	return nil
}
