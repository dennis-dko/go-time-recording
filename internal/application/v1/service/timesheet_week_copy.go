package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WeekCopy is what copying a week did.
type WeekCopy struct {
	// Copied is how many entries were written, and Hours what they add up to.
	Copied int
	Hours  float64

	// Kept is how many days were left as they stood because something was
	// already booked on them.
	Kept int

	// Closed is how many entries were left out because their project no longer
	// takes bookings from this person.
	Closed int
}

// CopyWeek books somebody's entries of one week into another, day for day: a
// Monday's onto the Monday, with the same project, hours and description.
//
// Only into days with nothing booked on them yet. A day somebody has already
// booked is theirs as it stands - adding last week's entries to it would count
// the day twice - so it is left alone, and the answer says how many were.
//
// An entry whose project no longer takes bookings is left out instead of refusing
// the copy: the week a project was finished would otherwise be one that could
// never be copied again. Everything else is held to the rules a booking is held
// to, the daily cap counting what the copy itself puts on a day, and written in
// one transaction or not at all.
func (s *TimesheetApplicationService) CopyWeek(
	ctx context.Context,
	userID uint,
	from, to time.Time,
) (*WeekCopy, error) {
	var invalid []string

	if !model.OnTheCalendar(from) || from.Weekday() != time.Monday {
		invalid = append(invalid, "from")
	}

	if !model.OnTheCalendar(to) || to.Weekday() != time.Monday || model.CalendarDay(to).Equal(model.CalendarDay(from)) {
		invalid = append(invalid, "to")
	}

	if len(invalid) > 0 {
		return nil, apperror.InvalidFields(invalid...)
	}

	if _, err := s.userRepository.GetByID(ctx, userID); err != nil {
		return nil, err
	}

	from, to = model.CalendarDay(from), model.CalendarDay(to)

	source, err := s.weekOf(ctx, userID, from)
	if err != nil {
		return nil, err
	}

	if len(source) == 0 {
		return nil, apperror.Invalidf("nothing is booked in that week, so there is nothing to copy").
			WithCode("nothingToCopy")
	}

	target, err := s.weekOf(ctx, userID, to)
	if err != nil {
		return nil, err
	}

	booked := map[time.Time]bool{}
	for _, entry := range target {
		booked[model.CalendarDay(entry.Date)] = true
	}

	// In the order they were worked, so the copies are listed the way the
	// originals are.
	slices.SortFunc(source, func(a, b *model.Timesheet) int {
		return cmp.Or(a.Date.Compare(b.Date), cmp.Compare(a.ID, b.ID))
	})

	var (
		copies  []*model.Timesheet
		kept    = map[time.Time]bool{}
		planned = map[time.Time]float64{}
		open    = s.stillOpen(ctx, userID)
		outcome WeekCopy
	)

	shift := to.Sub(from)

	for _, entry := range source {
		day := model.CalendarDay(entry.Date).Add(shift)

		if booked[day] {
			kept[day] = true

			continue
		}

		if entry.ProjectID != nil {
			takes, err := open(*entry.ProjectID)
			if err != nil {
				return nil, err
			}

			if !takes {
				outcome.Closed++

				continue
			}
		}

		if err := validateTimesheet(day, entry.DurationHours, entry.Description); err != nil {
			return nil, err
		}

		// The cap counts what the copy has already put on this day, the way the
		// importer counts a file against itself - each check alone would be made
		// against a database that knows nothing of the others.
		if err := s.checkDailyBudget(ctx, userID, day, entry.DurationHours+planned[day], 0); err != nil {
			return nil, err
		}

		planned[day] += entry.DurationHours

		copies = append(copies, &model.Timesheet{
			UserID:        userID,
			ProjectID:     entry.ProjectID,
			Date:          day,
			DurationHours: entry.DurationHours,
			Description:   entry.Description,
		})
	}

	if len(copies) > 0 {
		if err := s.timesheetRepository.SaveMany(ctx, copies); err != nil {
			return nil, err
		}
	}

	for _, entry := range copies {
		s.record(ctx, MetricHoursBooked, entry.DurationHours)

		outcome.Hours += entry.DurationHours
	}

	outcome.Copied, outcome.Kept, outcome.Hours = len(copies), len(kept), settled(outcome.Hours)

	return &outcome, nil
}

// weekOf returns somebody's entries in the seven days from monday.
func (s *TimesheetApplicationService) weekOf(
	ctx context.Context,
	userID uint,
	monday time.Time,
) ([]*model.Timesheet, error) {
	sunday := monday.AddDate(0, 0, 6)

	return s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		UserID: userID, StartDate: &monday, EndDate: &sunday,
	})
}

// stillOpen answers whether a project still takes bookings from somebody, asking
// about each project once however many entries name it.
//
// One that is gone, finished or no longer theirs to see is closed alike: the copy
// leaves its entries out, which is all the difference would decide.
func (s *TimesheetApplicationService) stillOpen(ctx context.Context, userID uint) func(uint) (bool, error) {
	known := map[uint]bool{}

	return func(projectID uint) (bool, error) {
		if takes, asked := known[projectID]; asked {
			return takes, nil
		}

		project, err := s.projectRepository.GetByID(ctx, projectID)

		switch {
		case apperror.KindOf(err) == apperror.KindNotFound:
			known[projectID] = false
		case err != nil:
			return false, err
		default:
			known[projectID] = domainservice.RequireVisible(project, userID) == nil &&
				project.Status == model.ProjectStatusActive
		}

		return known[projectID], nil
	}
}
