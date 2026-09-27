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

// A range still ends on its last day once a database has stored its end.
//
// The end used to be the last nanosecond of the last day, which is right in Go
// and wrong in both server databases: PostgreSQL keeps a timestamp to the
// microsecond and MySQL's DATETIME to the second, and each rounds rather than
// cuts - so 23:59:59.999999999 arrived as midnight of the next day, which is
// exactly where every entry of that day is stored. On both, a list from the first
// to the third showed the fourth, the daily limit counted tomorrow's hours as
// today's, and every total carried the day after its range. SQLite compares to
// the nanosecond and never showed it, and neither did the case above, because Go
// does not round.
func TestARangeEndsOnItsLastDayAtAnyPrecision(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)

	narrowed := repository.TimesheetFilter{StartDate: &from, EndDate: &to}.OverWholeDays()

	for name, precision := range map[string]time.Duration{
		"PostgreSQL, to the microsecond": time.Microsecond,
		"MySQL DATETIME, to the second":  time.Second,
	} {
		stored := narrowed.EndDate.Round(precision)

		if !stored.Before(nextDay) {
			t.Errorf("%s: the end is stored as %s, which takes in the entries of the "+
				"day after the range", name, stored.Format(time.RFC3339Nano))
		}

		if stored.Before(lastDay) {
			t.Errorf("%s: the end is stored as %s, before the range's own last day",
				name, stored.Format(time.RFC3339Nano))
		}
	}

	// And not simply the last day's midnight, which would round nowhere and would
	// drop the oldest rows: an entry recorded before the stored date lost its zone
	// carries the zone it was booked in, and midnight twelve hours west of UTC is
	// noon UTC.
	westernMidnight := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	if westernMidnight.After(*narrowed.EndDate) {
		t.Errorf("an entry of the last day booked in a zone west of UTC, stored as %s, "+
			"falls outside a range ending at %s", westernMidnight.Format(time.RFC3339),
			narrowed.EndDate.Format(time.RFC3339Nano))
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
