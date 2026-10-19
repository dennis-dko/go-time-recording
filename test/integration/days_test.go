//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

type daysOnTheWire struct {
	From        string  `json:"from"`
	To          string  `json:"to"`
	TotalBooked float64 `json:"totalBooked"`
	TotalTarget float64 `json:"totalTarget"`
	Days        []struct {
		Date    string  `json:"date"`
		Booked  float64 `json:"booked"`
		Target  float64 `json:"target"`
		Absence string  `json:"absence"`
	} `json:"days"`
}

// Every day of a range comes back, the empty ones included, with what it asked -
// a Saturday nothing, a Monday the daily target.
func TestTheDaysOfAWeekAreAnsweredWithTheirTargets(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	bookHours(t, worker, "2026-08-03", 8) // Monday
	bookHours(t, worker, "2026-08-08", 2) // Saturday

	var week daysOnTheWire

	worker.must(worker.api(http.MethodGet, "/me/days?from=2026-08-03&to=2026-08-09", nil),
		http.StatusOK).Data(t, &week)

	if len(week.Days) != 7 {
		t.Fatalf("a week came back as %d day(s), want 7", len(week.Days))
	}

	monday, saturday := week.Days[0], week.Days[5]

	if monday.Date != "2026-08-03" || monday.Booked != 8 || monday.Target != 8 {
		t.Errorf("the Monday reads %+v", monday)
	}

	if saturday.Date != "2026-08-08" || saturday.Booked != 2 || saturday.Target != 0 {
		t.Errorf("the Saturday reads %+v", saturday)
	}

	if week.TotalBooked != 10 || week.TotalTarget != 40 {
		t.Errorf("the week adds up to %.2f of %.2f, want 10 of 40", week.TotalBooked, week.TotalTarget)
	}
}

// Without a range it is this week, Monday to Sunday.
func TestTheDaysAreThisWeekUnlessAsked(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var week daysOnTheWire

	worker.must(worker.api(http.MethodGet, "/me/days", nil), http.StatusOK).Data(t, &week)

	if len(week.Days) != 7 {
		t.Fatalf("this week came back as %d day(s), want 7", len(week.Days))
	}

	first, err := time.Parse(time.DateOnly, week.Days[0].Date)
	if err != nil || first.Weekday() != time.Monday {
		t.Errorf("this week begins on %s, want a Monday", week.Days[0].Date)
	}
}

// A range longer than a year of weeks is refused by name rather than answered a
// day at a time for as long as somebody asks.
func TestTheDaysRefuseMoreThanAYearOfWeeks(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	refused := worker.api(http.MethodGet, "/me/days?from=2026-01-01&to=2027-02-01", nil)

	if refused.Status != http.StatusBadRequest || errorCode(t, refused) != "rangeTooLong" {
		t.Errorf("thirteen months answered %d %q, want 400 rangeTooLong", refused.Status, errorCode(t, refused))
	}
}
