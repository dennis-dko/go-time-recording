package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

type TimesheetFilter struct {
	UserID    uint
	ProjectID uint
	StartDate *time.Time
	EndDate   *time.Time

	// WithoutProject selects only the entries that have no project yet.
	// A zero ProjectID means "any project", so a separate flag is needed to
	// ask for the uncategorised ones.
	WithoutProject bool

	// Limit is how many entries to return and Offset how many to skip first.
	//
	// Zero Limit means no bound, which is what every caller that totals or exports
	// wants: a report over a year has to read the year. It is the *listing* that
	// must not, and the bound is applied by whoever is answering a screen rather
	// than by every reader of this filter. Offset without Limit is meaningless and
	// is ignored.
	//
	// Both are only well defined because GetByFilter has always returned a stable
	// order, newest day first and ties broken by id. Paging an unordered query
	// shows one entry twice and hides another, and the two look identical from the
	// outside.
	Limit  uint
	Offset uint
}

// OverWholeDays widens the range to the calendar days its ends fall on, and is
// what every repository must apply before comparing it to a stored date.
//
// An entry's date is a calendar day. It is written as midnight UTC and read back
// the same way, because "which day did you work" has no time of day in it and no
// zone either - the answer is 3 July whether it is read in Berlin or in Los
// Angeles.
//
// The ends of a range do not arrive that way. They are built from the reader's
// own clock, correctly: "this month" has to mean the month it is where the
// person is, and "the last thirty days" has to end on their today rather than
// the server's. So the range arrives as two instants carrying a zone, and it was
// compared straight against dates that carry none.
//
// East of UTC that happened to work. West of it, it silently dropped a day at
// each end: the first of the month in Los Angeles is 07:00 UTC, every entry
// stored for that day is midnight UTC, and midnight is before seven - so the
// first day of every month was missing from that person's balance, and the day
// after their today was in it. Nothing on screen looked wrong. The month simply
// started on the second.
//
// The calendar fields are read in the range's own location, which is the whole
// point: 1 July in Los Angeles is 1 July, and it is then expressed the way a
// stored date is.
func (f TimesheetFilter) OverWholeDays() TimesheetFilter {
	if f.StartDate != nil {
		start := model.CalendarDay(*f.StartDate)
		f.StartDate = &start
	}

	if f.EndDate != nil {
		end := model.CalendarDay(*f.EndDate).AddDate(0, 0, 1).Add(-time.Nanosecond)
		f.EndDate = &end
	}

	return f
}

// TimesheetRepository repository functions for timesheet
type TimesheetRepository interface {
	Save(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error)

	// SaveMany writes several entries as one change: either all of them land or
	// none do.
	//
	// For the spreadsheet import, which writes a whole file. Half a file in the
	// database is worse than none: nobody can tell which half, or which entries
	// came from it and which were already there.
	SaveMany(ctx context.Context, entries []*model.Timesheet) error

	GetByID(ctx context.Context, id uint) (*model.Timesheet, error)

	GetByFilter(ctx context.Context, filter TimesheetFilter) ([]*model.Timesheet, error)

	// CountByFilter is how many entries match, ignoring Limit and Offset.
	//
	// Separate from GetByFilter rather than derived from it, because the number a
	// page has to report is the size of the whole match and counting the rows that
	// came back would report the size of the page. Reading every row to count them
	// is the cost this exists to avoid.
	CountByFilter(ctx context.Context, filter TimesheetFilter) (uint, error)

	GetAll(ctx context.Context) ([]*model.Timesheet, error)

	Update(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error)

	Delete(ctx context.Context, id uint) error
}
