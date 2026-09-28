//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// A day sent with a time of day and an offset is stored as the day it names.
//
// The API takes a date as "2006-01-02" or as a full RFC 3339 timestamp, and says
// clients never have to reason about the time of day. A timestamp whose day in
// UTC is not the day it was written as is exactly where that promise is tested:
// "2026-07-03T01:00:00+02:00" is the 3rd where it was written and the 2nd in UTC,
// and "2026-07-03T23:30:00-05:00" is the 3rd where it was written and the 4th in
// UTC. The day a booking belongs to is the day somebody wrote, so both belong to
// the 3rd - on every dialect, because a stored date is midnight UTC of that day
// and carries no zone.
func TestADaySentWithATimeIsStoredAsTheDayItNames(t *testing.T) {
	t.Parallel()

	_, _, wera := startWithWorker(t)

	for _, sent := range []string{"2026-07-03T01:00:00+02:00", "2026-07-03T23:30:00-05:00"} {
		var created timesheetResponse

		wera.must(wera.api(http.MethodPost, "/timesheets", map[string]any{
			"date": sent, "durationHours": 1,
		}), http.StatusCreated, http.StatusOK).Data(t, &created)

		if created.Date != "2026-07-03" {
			t.Errorf("%s was answered as %s, want 2026-07-03", sent, created.Date)
		}
	}

	var onTheDay struct {
		Items []timesheetResponse `json:"items"`
	}

	wera.must(wera.api(http.MethodGet, "/timesheets?from=2026-07-03&to=2026-07-03", nil),
		http.StatusOK).Data(t, &onTheDay)

	if len(onTheDay.Items) != 2 {
		t.Errorf("the 3rd of July holds %d entries, want the 2 booked on it", len(onTheDay.Items))
	}

	for _, entry := range onTheDay.Items {
		if entry.Date != "2026-07-03" {
			t.Errorf("an entry on the 3rd of July reads as %s", entry.Date)
		}
	}
}

// A range of days ends on its last day, on every dialect.
//
// The end of a range was the last nanosecond of its last day. PostgreSQL keeps a
// timestamp to the microsecond and MySQL's DATETIME to the second, both round,
// and 23:59:59.999999999 became midnight of the next day - where every entry of
// that day is stored. So on both server dialects a list from the first to the
// third showed the fourth, the statistics for those days counted it, and the
// daily limit refused a booking because of the hours booked for tomorrow. SQLite
// compares to the nanosecond and this suite's SQLite leg passed throughout.
func TestARangeOfDaysEndsOnItsLastDay(t *testing.T) {
	t.Parallel()

	a := start(t, "MAX_DAILY_HOURS=10")
	admin := a.signInAsAdmin("a-much-better-password")
	wera := a.signInAsUser(admin, "Wera", "wera@example.com")

	for date, hours := range map[string]float64{"2026-07-03": 2, "2026-07-04": 8} {
		wera.must(wera.api(http.MethodPost, "/timesheets", map[string]any{
			"date": date, "durationHours": hours,
		}), http.StatusCreated, http.StatusOK)
	}

	var listed struct {
		Items []timesheetResponse `json:"items"`
	}

	wera.must(wera.api(http.MethodGet, "/timesheets?from=2026-07-01&to=2026-07-03", nil),
		http.StatusOK).Data(t, &listed)

	for _, entry := range listed.Items {
		if entry.Date != "2026-07-03" {
			t.Errorf("the list from the 1st to the 3rd of July holds an entry on %s", entry.Date)
		}
	}

	if stats := ownStatistics(t, wera, "?from=2026-07-01&to=2026-07-03"); stats.TotalHours != 2 {
		t.Errorf("the statistics from the 1st to the 3rd total %v hours, want the 2 "+
			"booked on the 3rd", stats.TotalHours)
	}

	// Eight more on the 3rd make ten there, which is the limit and allowed - the
	// eight already on the 4th are another day's.
	if got := wera.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-07-03", "durationHours": 8,
	}).Status; got != http.StatusCreated && got != http.StatusOK {
		t.Errorf("booking up to the limit on the 3rd was refused with %d, because the "+
			"4th's hours were counted as the 3rd's", got)
	}
}

// A day reads as itself when the database answers in a zone west of UTC.
//
// A stored day is midnight UTC, and both server dialects hand it back in a zone
// the application did not choose: PostgreSQL in the session's zone, which
// PGOPTIONS sets here (lib/pq reads PGTZ but never sends it, so that variable
// changes nothing), and MySQL in the process's own, which GoFr asks for with
// loc=Local and TZ sets on Linux. West of UTC that is the evening before, and
// every entry read back as the day before it was booked - on the list, in the
// statistics and in everything built from them. SQLite answers in the zone the
// value was written with and was never affected, so that leg passes either way;
// TZ does nothing on Windows, where the MySQL half is only measured by CI.
func TestADayReadsAsItselfWestOfUTC(t *testing.T) {
	t.Parallel()

	a := start(t, "PGOPTIONS=-c timezone=America/New_York", "TZ=America/New_York")
	admin := a.signInAsAdmin("a-much-better-password")
	wera := a.signInAsUser(admin, "Wera", "wera@example.com")

	wera.must(wera.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-07-03", "durationHours": 2,
	}), http.StatusCreated, http.StatusOK)

	var listed struct {
		Items []timesheetResponse `json:"items"`
	}

	wera.must(wera.api(http.MethodGet, "/timesheets?from=2026-07-01&to=2026-07-31", nil),
		http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 1 || listed.Items[0].Date != "2026-07-03" {
		t.Errorf("the entry booked on the 3rd of July is listed as %+v", listed.Items)
	}

	stats := ownStatistics(t, wera, "?from=2026-07-01&to=2026-07-31")

	for _, day := range stats.Days {
		want := 0.0
		if day.Date == "2026-07-03" {
			want = 2
		}

		if day.Hours != want {
			t.Errorf("the statistics give %s %v hours, want %v", day.Date, day.Hours, want)
		}
	}
}

// A day no calendar on this screen could show is refused as a mistyped date.
//
// "0202-08-03" is a few keystrokes from "2026-08-03" in a date field, and it was
// accepted and stored on all three dialects - an entry in the year 202, where no
// range anybody looks at will ever show it. The spreadsheet importer reads only
// days a sheet can hold, 1900 to 9999, and bookings now keep to the same.
func TestADayNoCalendarCouldShowIsRefused(t *testing.T) {
	t.Parallel()

	_, _, wera := startWithWorker(t)

	for _, sent := range []string{"0202-08-03", "1899-12-31", "10000-01-01T00:00:00Z"} {
		if got := wera.api(http.MethodPost, "/timesheets", map[string]any{
			"date": sent, "durationHours": 1,
		}).Status; got != http.StatusBadRequest {
			t.Errorf("a booking on %s answered %d, want %d", sent, got, http.StatusBadRequest)
		}

		// A project's dates are days of the same calendar.
		if got := wera.api(http.MethodPost, "/projects", map[string]any{
			"name": "Somewhere in time", "startDate": sent,
		}).Status; got != http.StatusBadRequest {
			t.Errorf("a project starting %s answered %d, want %d", sent, got, http.StatusBadRequest)
		}
	}
}
