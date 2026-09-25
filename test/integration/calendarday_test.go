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
