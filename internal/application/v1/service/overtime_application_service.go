package service

import (
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

// OvertimeService computes overtime balances.
type OvertimeService struct {
	timesheets repository.TimesheetRepository
	users      repository.UserRepository
}

// NewOvertimeService creates new instance.
func NewOvertimeService(
	timesheets repository.TimesheetRepository,
	users repository.UserRepository,
) *OvertimeService {
	return &OvertimeService{timesheets: timesheets, users: users}
}

// Balance computes the overtime balance for one user over a date range.
//
// Only days the user actually booked on count towards the target. Counting
// every calendar day would turn weekends, holidays and leave into a growing
// deficit, which would make the figure meaningless without a holiday calendar
// this application does not have.
//
// Entries that were rejected are excluded: they represent work that was not
// accepted, so counting them would overstate the balance.
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

	start := startOfDay(from)
	end := endOfDay(to)

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &start,
		EndDate:   &end,
	})
	if err != nil {
		return nil, err
	}

	target := user.EffectiveDailyTarget()

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

	for day, booked := range bookedPerDay {
		balance.Days = append(balance.Days, OvertimeDay{
			Date:    day,
			Booked:  booked,
			Target:  target,
			Balance: booked - target,
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

	return balance, nil
}

// BalanceForAll computes the balance of every user, for a team overview.
func (s *OvertimeService) BalanceForAll(
	ctx context.Context,
	from, to time.Time,
) ([]*OvertimeBalance, error) {
	users, err := s.users.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	balances := make([]*OvertimeBalance, 0, len(users))

	for _, user := range users {
		balance, balErr := s.Balance(ctx, user.ID, from, to)
		if balErr != nil {
			return nil, balErr
		}

		balances = append(balances, balance)
	}

	return balances, nil
}

func endOfDay(t time.Time) time.Time {
	return startOfDay(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}
