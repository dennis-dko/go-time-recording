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
