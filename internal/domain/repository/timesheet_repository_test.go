package repository_test

import (
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// A range is a range of days, whatever zone its ends were built in.
//
// An entry's date is a calendar day, written as midnight UTC. The ends of a
// range are not: they are built from the reader's own clock, and correctly so -
// "this month" means the month it is where the person is. So the two met as an
// instant against a date, and west of UTC that lost a day at each end.
//
// Los Angeles is the case that showed it. The first of the month there is 07:00
// UTC; every entry stored for that day is midnight UTC; midnight is before
// seven. The balance simply started on the second, and nothing on any screen
// looked wrong.
func TestARangeCoversTheDaysItsEndsFallOn(t *testing.T) {
	west, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("this machine has no zone database")
	}

	east, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skip("this machine has no zone database")
	}

	// The day an entry booked on 1 July is stored as, and the one after the end.
	first := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	afterLast := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	for name, zone := range map[string]*time.Location{
		"west of UTC": west,
		"east of UTC": east,
		"UTC itself":  time.UTC,
	} {
		t.Run(name, func(t *testing.T) {
			// What a handler builds when nobody passed a range: the first of the
			// month, and the reader's own now.
			from := time.Date(2026, 7, 1, 0, 0, 0, 0, zone)
			to := time.Date(2026, 7, 20, 11, 30, 0, 0, zone)

			narrowed := repository.TimesheetFilter{StartDate: &from, EndDate: &to}.OverWholeDays()

			if first.Before(*narrowed.StartDate) {
				t.Errorf("an entry on the first of the month falls outside a range "+
					"that starts on the first of the month: %s is before %s",
					first.Format(time.RFC3339), narrowed.StartDate.Format(time.RFC3339))
			}

			if !afterLast.After(*narrowed.EndDate) {
				t.Errorf("an entry dated the day after the range's last day is inside "+
					"it: %s is not after %s",
					afterLast.Format(time.RFC3339), narrowed.EndDate.Format(time.RFC3339))
			}

			// And the last day itself is in, which is the half a narrowing can
			// break while fixing the other one.
			last := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
			if last.After(*narrowed.EndDate) {
				t.Errorf("the range's own last day fell outside it: %s is after %s",
					last.Format(time.RFC3339), narrowed.EndDate.Format(time.RFC3339))
			}
		})
	}
}

// Nothing to narrow is left alone: an open-ended range stays open-ended rather
// than becoming a range around the zero time.
func TestAnOpenRangeStaysOpen(t *testing.T) {
	narrowed := repository.TimesheetFilter{UserID: 7}.OverWholeDays()

	if narrowed.StartDate != nil || narrowed.EndDate != nil {
		t.Errorf("a filter with no dates came back with %v..%v",
			narrowed.StartDate, narrowed.EndDate)
	}

	if narrowed.UserID != 7 {
		t.Errorf("narrowing the range changed the rest of the filter: %+v", narrowed)
	}
}
