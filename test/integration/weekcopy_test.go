//go:build integration

package integration

import (
	"net/http"
	"slices"
	"testing"
)

type weekCopyOnTheWire struct {
	Copied int     `json:"copied"`
	Hours  float64 `json:"hours"`
	Kept   int     `json:"kept"`
	Closed int     `json:"closed"`
}

// bookedOn lists the hours somebody has on one day, in the order they were booked.
func bookedOn(t *testing.T, c *client, day string) []float64 {
	t.Helper()

	var list listOf[timesheetResponse]

	c.must(c.api(http.MethodGet, "/timesheets?from="+day+"&to="+day, nil), http.StatusOK).Data(t, &list)

	slices.SortFunc(list.Items, func(a, b timesheetResponse) int { return int(a.ID) - int(b.ID) })

	hours := make([]float64, 0, len(list.Items))
	for _, entry := range list.Items {
		hours = append(hours, entry.DurationHours)
	}

	return hours
}

// A week is copied into the days of the next that have nothing on them, on every
// database: a day already booked keeps what it had, and a colleague's entries in
// the same week are not the caller's to copy.
func TestAWeekIsCopiedIntoTheDaysWithNothingOnThem(t *testing.T) {
	t.Parallel()

	_, anna, bert := twoColleagues(t)

	var portal projectResponse

	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Portal", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &portal)

	entryOf(t, anna, map[string]any{
		"date": "2026-08-03", "durationHours": 4, "projectId": portal.ID, "description": "release",
	})
	bookHours(t, anna, "2026-08-04", 2)
	bookHours(t, anna, "2026-08-11", 1) // the Tuesday being copied into, already booked
	bookHours(t, bert, "2026-08-05", 7) // the colleague's, in the week being copied

	var copied weekCopyOnTheWire

	anna.must(anna.api(http.MethodPost, "/me/weeks/copy", map[string]any{
		"from": "2026-08-03", "to": "2026-08-10",
	}), http.StatusCreated, http.StatusOK).Data(t, &copied)

	if copied != (weekCopyOnTheWire{Copied: 1, Hours: 4, Kept: 1}) {
		t.Errorf("the copy answered %+v, want the Monday's four hours copied and the Tuesday kept", copied)
	}

	if monday := bookedOn(t, anna, "2026-08-10"); !slices.Equal(monday, []float64{4}) {
		t.Errorf("the Monday holds %v, want last Monday's four hours", monday)
	}

	if tuesday := bookedOn(t, anna, "2026-08-11"); !slices.Equal(tuesday, []float64{1}) {
		t.Errorf("the Tuesday holds %v, want the hour that was there and nothing more", tuesday)
	}

	if wednesday := bookedOn(t, anna, "2026-08-12"); len(wednesday) != 0 {
		t.Errorf("the Wednesday holds %v, want nothing - the seven hours were a colleague's", wednesday)
	}
}

// Weeks are named by their Mondays, and an empty week is refused by name.
func TestACopyNeedsMondaysAndSomethingToCopy(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	tuesday := worker.api(http.MethodPost, "/me/weeks/copy", map[string]any{
		"from": "2026-08-04", "to": "2026-08-10",
	})

	if tuesday.Status != http.StatusBadRequest {
		t.Errorf("a week named by its Tuesday answered %d, want 400", tuesday.Status)
	}

	empty := worker.api(http.MethodPost, "/me/weeks/copy", map[string]any{
		"from": "2026-08-03", "to": "2026-08-10",
	})

	if empty.Status != http.StatusBadRequest || errorCode(t, empty) != "nothingToCopy" {
		t.Errorf("an empty week answered %d %q, want 400 nothingToCopy", empty.Status, errorCode(t, empty))
	}
}
