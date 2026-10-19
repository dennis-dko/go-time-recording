package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// AbsenceService keeps the days somebody records themselves as away.
//
// Each person's own: nobody records anybody else as ill, and nobody reads that
// somebody was - the same reasoning that keeps a balance somebody's own.
type AbsenceService struct {
	absences  repository.AbsenceRepository
	users     repository.UserRepository
	schedules repository.WorkScheduleRepository
	regions   RegionSource
}

// NewAbsenceService needs the accounts, their planned weeks and the
// installation's holidays as well, because an absence is only recorded on a day
// that would otherwise be worked.
func NewAbsenceService(
	absences repository.AbsenceRepository,
	users repository.UserRepository,
	schedules repository.WorkScheduleRepository,
	regions RegionSource,
) *AbsenceService {
	return &AbsenceService{absences: absences, users: users, schedules: schedules, regions: regions}
}

// Absences returns somebody's absences from one day to another, earliest first.
func (s *AbsenceService) Absences(ctx context.Context, userID uint, from, to time.Time) ([]model.Absence, error) {
	first, last, err := dayRange(from, to)
	if err != nil {
		return nil, err
	}

	return s.absences.Between(ctx, userID, first, last)
}

// Record marks the working days from one day to another as away, for one reason,
// replacing whatever reason any of them had.
//
// Only the days somebody's week asks something of and that are no public holiday
// where they work: leave from a Monday to the Friday after next is ten days of
// it, not the twelve the calendar counts - and nine with Whit Monday in it - and a
// weekend recorded as illness is one more thing to read past. A range with no
// such day in it is refused by name rather than answered with nothing done.
func (s *AbsenceService) Record(
	ctx context.Context,
	userID uint,
	from, to time.Time,
	kind string,
) ([]model.Absence, error) {
	var invalid []string

	if !model.OnTheCalendar(from) {
		invalid = append(invalid, "from")
	}

	if !model.OnTheCalendar(to) {
		invalid = append(invalid, "to")
	}

	if !model.ValidAbsenceKind(kind) {
		invalid = append(invalid, "kind")
	}

	if len(invalid) > 0 {
		return nil, apperror.InvalidFields(invalid...)
	}

	first, last, err := dayRange(from, to)
	if err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	schedules, err := s.schedules.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	holidays, err := holidaysOf(ctx, s.regions, user, first, last)
	if err != nil {
		return nil, err
	}

	before := model.DefaultWeek(user.EffectiveDailyTarget())

	var absences []model.Absence

	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		if _, holiday := holidays[day]; holiday || model.TargetOn(day, schedules, before) <= 0 {
			continue
		}

		absences = append(absences, model.Absence{UserID: userID, Day: day, Kind: kind})
	}

	if len(absences) == 0 {
		return nil, apperror.Invalidf("no day in that range is a working day, so there is nothing to record").
			WithCode("noWorkingDays")
	}

	if err := s.absences.Save(ctx, absences); err != nil {
		return nil, err
	}

	return absences, nil
}

// Remove takes back the absence recorded on a day.
func (s *AbsenceService) Remove(ctx context.Context, userID uint, day time.Time) error {
	found, err := s.absences.Delete(ctx, userID, day)
	if err != nil {
		return err
	}

	if !found {
		return apperror.NotFound("absence", model.CalendarDay(day).Format(time.DateOnly))
	}

	return nil
}
