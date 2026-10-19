package service

import (
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// OvertimeDay is the balance for one calendar day.
type OvertimeDay struct {
	Date    time.Time
	Booked  float64
	Target  float64
	Balance float64 // Booked - Target
}

// OvertimeBalance summarises a user's overtime across a period.
type OvertimeBalance struct {
	UserID   uint
	UserName string
	From     time.Time
	To       time.Time

	DailyTarget float64
	Days        []OvertimeDay

	TotalBooked  float64
	TotalTarget  float64
	TotalBalance float64
}

// Day is one calendar day of somebody's: what was booked on it and what it asked.
type Day struct {
	Date   time.Time
	Booked float64
	Target float64

	// Absence is the kind of absence recorded on the day, or empty.
	Absence string

	// Holiday is the public holiday the day is where this person works, or empty.
	Holiday string
}

// MaxDays is the longest range Days answers: fifty-three weeks, which is what a
// year drawn as whole weeks from Monday to Sunday can span.
const MaxDays = 371

// dayRange reads a range of days, refusing one that ends before it begins or
// spans more than MaxDays - the bounds every answer given a day at a time keeps.
func dayRange(from, to time.Time) (first, last time.Time, err error) {
	first, last = model.CalendarDay(from), model.CalendarDay(to)

	if last.Before(first) {
		return first, last, apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted")
	}

	if span := int(last.Sub(first).Hours()/24) + 1; span > MaxDays {
		return first, last, apperror.Invalidf("a range of days may span at most %d days", MaxDays).
			WithCode("rangeTooLong", MaxDays)
	}

	return first, last, nil
}

// OvertimeService computes overtime balances.
type OvertimeService struct {
	timesheets repository.TimesheetRepository
	users      repository.UserRepository
	schedules  repository.WorkScheduleRepository
	absences   repository.AbsenceRepository
	regions    RegionSource
}

// NewOvertimeService needs the accounts, their planned weeks, their absences and
// the installation's holidays as well as their hours, because what a day asks is
// each account's own.
func NewOvertimeService(
	timesheets repository.TimesheetRepository,
	users repository.UserRepository,
	schedules repository.WorkScheduleRepository,
	absences repository.AbsenceRepository,
	regions RegionSource,
) *OvertimeService {
	return &OvertimeService{
		timesheets: timesheets, users: users, schedules: schedules, absences: absences, regions: regions,
	}
}

// Balance computes the overtime balance for one user over a date range.
//
// Only days the user actually booked on count towards the target. Counting
// every calendar day would turn weekends, holidays and leave into a growing
// deficit, which would make the figure meaningless without a holiday calendar
// this application does not have.
//
// A booked day is measured against the week in force on it - see model.TargetOn -
// so a day the week leaves free counts every hour booked on it as overtime.
// DailyTarget stays the account's own figure, which is what that week is made of
// where no plan has been made.
func (s *OvertimeService) Balance(
	ctx context.Context,
	userID uint,
	from, to time.Time,
) (*OvertimeBalance, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	if to.Before(from) {
		return nil, apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// A range of days, whose end the repository decides; see OverWholeDays.
	start := startOfDay(from)
	end := startOfDay(to)

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &start,
		EndDate:   &end,
	})
	if err != nil {
		return nil, err
	}

	target := user.EffectiveDailyTarget()

	// What each day asks is the week planned for it, and before any plan the
	// daily target from Monday to Friday - not the daily target on every day,
	// which counted a Saturday's work as a shortfall.
	schedules, err := s.schedules.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	before := model.DefaultWeek(target)

	bookedPerDay := make(map[time.Time]float64)

	// Keyed by the calendar day rather than by the value in the column, because a
	// database that has been in use has both shapes in it: every stopwatch entry
	// written before BookingDay was corrected carries the zone it was recorded in.
	// Grouping by the raw value puts those in a day of their own, beside the same
	// day typed by hand, and charges a full target to each.
	for _, entry := range entries {
		bookedPerDay[model.CalendarDay(entry.Date)] += entry.DurationHours
	}

	balance := &OvertimeBalance{
		UserID:      user.ID,
		UserName:    user.Name,
		From:        start,
		To:          startOfDay(to),
		DailyTarget: target,
		Days:        make([]OvertimeDay, 0, len(bookedPerDay)),
	}

	// Settled, every figure that is a sum or a difference: see settled.
	for day, booked := range bookedPerDay {
		planned := model.TargetOn(day, schedules, before)

		balance.Days = append(balance.Days, OvertimeDay{
			Date:    day,
			Booked:  settled(booked),
			Target:  planned,
			Balance: settled(booked - planned),
		})
	}

	// Map iteration order is random; sort so the result is stable.
	slices.SortFunc(balance.Days, func(a, b OvertimeDay) int {
		return a.Date.Compare(b.Date)
	})

	for _, day := range balance.Days {
		balance.TotalBooked += day.Booked
		balance.TotalTarget += day.Target
		balance.TotalBalance += day.Balance
	}

	balance.TotalBooked = settled(balance.TotalBooked)
	balance.TotalTarget = settled(balance.TotalTarget)
	balance.TotalBalance = settled(balance.TotalBalance)

	return balance, nil
}

// Days returns every day of a range with what was booked on it and what it asked.
//
// Every day, the empty ones included, which is the difference from Balance: the
// balance counts only the days somebody worked, so that a day off does not stand
// as a shortfall, while a picture of a week has to show the Tuesday nobody booked
// on as well. What a day asks is the week in force on it, as in the balance.
//
// Bounded to MaxDays, so the range is a picture's worth and not a history's.
func (s *OvertimeService) Days(ctx context.Context, userID uint, from, to time.Time) ([]Day, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
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

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &first,
		EndDate:   &last,
	})
	if err != nil {
		return nil, err
	}

	booked := make(map[time.Time]float64)
	for _, entry := range entries {
		booked[model.CalendarDay(entry.Date)] += entry.DurationHours
	}

	away, err := s.absences.Between(ctx, userID, first, last)
	if err != nil {
		return nil, err
	}

	absent := make(map[time.Time]string, len(away))
	for _, absence := range away {
		absent[absence.Day] = absence.Kind
	}

	holidays, err := holidaysOf(ctx, s.regions, user, first, last)
	if err != nil {
		return nil, err
	}

	before := model.DefaultWeek(user.EffectiveDailyTarget())

	var days []Day

	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		days = append(days, Day{
			Date:    d,
			Booked:  settled(booked[d]),
			Target:  model.TargetOn(d, schedules, before),
			Absence: absent[d],
			Holiday: holidays[d].Name,
		})
	}

	return days, nil
}
