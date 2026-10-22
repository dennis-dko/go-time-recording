package service

import (
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WorkDayService keeps when somebody worked - when each day began and finished and
// how long its breaks were - and reads it beside the hours they booked.
//
// Each person's own, like their hours: a record of their working time kept for
// themselves, which nothing here shows anybody else.
type WorkDayService struct {
	days       repository.WorkDayRepository
	timesheets repository.TimesheetRepository
}

// NewWorkDayService needs the time entries, because a day is read beside what was
// booked on it.
func NewWorkDayService(days repository.WorkDayRepository, timesheets repository.TimesheetRepository) *WorkDayService {
	return &WorkDayService{days: days, timesheets: timesheets}
}

// WorkDayRecord is a day beside the hours booked on it.
type WorkDayRecord struct {
	model.WorkDay

	// Recorded says whether the day's times were recorded at all. A day only
	// booked on is read too, without them, because a record of working time with a
	// day missing is what somebody keeping one wants to find.
	Recorded bool

	BookedHours float64

	// Notices is what the Working Hours Act says about the shift that began on this
	// day; see model.WorkingTimeNotices.
	Notices []model.WorkingTimeNotice
}

// Between reads somebody's days from one day to another: every day they recorded
// or booked on, earliest first.
func (s *WorkDayService) Between(ctx context.Context, userID uint, from, to time.Time) ([]WorkDayRecord, error) {
	first, last, err := dayRange(from, to)
	if err != nil {
		return nil, err
	}

	// From the day before, for the rest before the range's first day; the day
	// itself is read for that and not returned.
	recorded, err := s.days.Between(ctx, userID, first.AddDate(0, 0, -1), last)
	if err != nil {
		return nil, err
	}

	notices := model.WorkingTimeNotices(recorded)

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID: userID, StartDate: &first, EndDate: &last,
	})
	if err != nil {
		return nil, err
	}

	byDay := make(map[time.Time]*WorkDayRecord)

	for _, day := range recorded {
		if day.Day.Before(first) {
			continue
		}

		byDay[day.Day] = &WorkDayRecord{WorkDay: day, Recorded: true, Notices: notices[day.Day]}
	}

	for _, entry := range entries {
		date := model.CalendarDay(entry.Date)

		record, found := byDay[date]
		if !found {
			record = &WorkDayRecord{WorkDay: model.WorkDay{UserID: userID, Day: date}}
			byDay[date] = record
		}

		record.BookedHours += entry.DurationHours
	}

	days := make([]WorkDayRecord, 0, len(byDay))
	for _, record := range byDay {
		record.BookedHours = settled(record.BookedHours)
		days = append(days, *record)
	}

	slices.SortFunc(days, func(a, b WorkDayRecord) int { return a.Day.Compare(b.Day) })

	return days, nil
}

// Record records a day, in place of what was recorded for it.
//
// A day begins before it finishes, inside the day - a night that runs past
// midnight is recorded as the two days it falls on - and its breaks are shorter
// than the time between.
func (s *WorkDayService) Record(ctx context.Context, day model.WorkDay) error {
	switch {
	case day.Day.IsZero() || !model.OnTheCalendar(day.Day):
		return apperror.InvalidFields("date")
	case day.StartMinute < 0 || day.StartMinute >= model.MinutesPerDay:
		return apperror.InvalidFields("start")
	case day.EndMinute <= day.StartMinute || day.EndMinute > model.MinutesPerDay:
		return apperror.InvalidFields("end")
	case day.BreakMinutes < 0 || day.BreakMinutes >= day.EndMinute-day.StartMinute:
		return apperror.InvalidFields("breakMinutes")
	}

	day.Day = model.CalendarDay(day.Day)

	return s.days.Save(ctx, day)
}

// Remove takes a recorded day back.
func (s *WorkDayService) Remove(ctx context.Context, userID uint, day time.Time) error {
	removed, err := s.days.Delete(ctx, userID, day)
	if err != nil {
		return err
	}

	if !removed {
		return apperror.NotFound("workday", model.CalendarDay(day).Format(time.DateOnly))
	}

	return nil
}
