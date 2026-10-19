//go:build integration

package integration

import (
	"net/http"
	"slices"
	"testing"
)

type absenceOnTheWire struct {
	Day  string `json:"day"`
	Kind string `json:"kind"`
}

func absencesOf(t *testing.T, c *client, from, to string) []absenceOnTheWire {
	t.Helper()

	var list listOf[absenceOnTheWire]

	c.must(c.api(http.MethodGet, "/me/absences?from="+from+"&to="+to, nil), http.StatusOK).Data(t, &list)

	return list.Items
}

// A week of leave is its five working days on every database, read back over a
// range that ends on its last day, and the days of that week say so.
func TestAWeekOfLeaveIsItsWorkingDays(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var recorded listOf[absenceOnTheWire]

	// 3 August 2026 is a Monday.
	worker.must(worker.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-08-03", "to": "2026-08-09", "kind": "vacation",
	}), http.StatusCreated, http.StatusOK).Data(t, &recorded)

	if recorded.TotalCount != 5 {
		t.Errorf("a week of leave was recorded as %d day(s), want its five working days", recorded.TotalCount)
	}

	// Ending on the Friday: a range takes in its last day.
	read := absencesOf(t, worker, "2026-08-03", "2026-08-07")

	var days []string
	for _, absence := range read {
		days = append(days, absence.Day)
	}

	if want := []string{"2026-08-03", "2026-08-04", "2026-08-05", "2026-08-06", "2026-08-07"}; !slices.Equal(days, want) {
		t.Errorf("the leave reads %v, want %v", days, want)
	}

	var week daysOnTheWire

	worker.must(worker.api(http.MethodGet, "/me/days?from=2026-08-03&to=2026-08-09", nil),
		http.StatusOK).Data(t, &week)

	if len(week.Days) != 7 || week.Days[0].Absence != "vacation" || week.Days[5].Absence != "" {
		t.Errorf("the days of the week read %+v, want the Monday on leave and the Saturday not", week.Days)
	}
}

// A day recorded again takes the new reason, and a day taken back is gone - and
// taken back twice, is not there.
func TestAnAbsenceIsReplacedAndTakenBack(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	for _, kind := range []string{"vacation", "sick"} {
		worker.must(worker.api(http.MethodPost, "/me/absences", map[string]any{
			"from": "2026-08-04", "to": "2026-08-04", "kind": kind,
		}), http.StatusCreated, http.StatusOK)
	}

	if read := absencesOf(t, worker, "2026-08-04", "2026-08-04"); len(read) != 1 || read[0].Kind != "sick" {
		t.Errorf("the Tuesday reads %+v, want one absence, sick", read)
	}

	worker.must(worker.api(http.MethodDelete, "/me/absences/2026-08-04", nil), http.StatusOK, http.StatusNoContent)

	if again := worker.api(http.MethodDelete, "/me/absences/2026-08-04", nil); again.Status != http.StatusNotFound {
		t.Errorf("taking back an absence twice answered %d, want 404", again.Status)
	}
}

// A colleague's absences are not there to read, and not there to take back.
func TestAColleaguesAbsencesAreNotThere(t *testing.T) {
	t.Parallel()

	_, anna, bert := twoColleagues(t)

	anna.must(anna.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-08-05", "to": "2026-08-05", "kind": "sick",
	}), http.StatusCreated, http.StatusOK)

	if seen := absencesOf(t, bert, "2026-08-01", "2026-08-31"); len(seen) != 0 {
		t.Errorf("a colleague reads %+v, want nothing of Anna's", seen)
	}

	if refused := bert.api(http.MethodDelete, "/me/absences/2026-08-05", nil); refused.Status != http.StatusNotFound {
		t.Errorf("taking back a colleague's absence answered %d, want 404", refused.Status)
	}

	if kept := absencesOf(t, anna, "2026-08-05", "2026-08-05"); len(kept) != 1 {
		t.Errorf("Anna's sick day is gone after a colleague tried to take it back")
	}
}

// An account's absences go with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsAbsencesWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	worker.must(worker.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-08-03", "to": "2026-08-04", "kind": "vacation",
	}), http.StatusCreated, http.StatusOK)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "absences", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d absence(s) outlived the account they belonged to", left)
	}
}
