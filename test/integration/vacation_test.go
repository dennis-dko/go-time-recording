//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type vacationOnTheWire struct {
	Year         int     `json:"year"`
	Entitlement  float64 `json:"entitlement"`
	CarriedOver  float64 `json:"carriedOver"`
	Taken        float64 `json:"taken"`
	Planned      float64 `json:"planned"`
	Remaining    float64 `json:"remaining"`
	Entitlements []struct {
		FromYear int     `json:"fromYear"`
		Days     float64 `json:"days"`
	} `json:"entitlements"`
}

func vacationIn(t *testing.T, c *client, year string) vacationOnTheWire {
	t.Helper()

	var account vacationOnTheWire

	c.must(c.api(http.MethodGet, "/me/vacation?year="+year, nil), http.StatusOK).Data(t, &account)

	return account
}

// A year's leave on every database: what it gives, what the year before left over,
// and what was taken of it - and what a colleague's account says about none of it.
func TestAYearsLeaveIsWhatItGivesLessWhatIsTaken(t *testing.T) {
	t.Parallel()

	_, anna, bert := twoColleagues(t)

	anna.must(anna.api(http.MethodPut, "/me/vacation", map[string]any{"fromYear": 2025, "days": 28}), http.StatusOK)

	// A week of 2025 taken, so 23 days are carried into 2026.
	anna.must(anna.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2025-12-01", "to": "2025-12-07", "kind": "vacation",
	}), http.StatusCreated, http.StatusOK)

	// Two and a half days of 2026, in the past whenever this runs.
	anna.must(anna.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-02-02", "to": "2026-02-03", "kind": "vacation",
	}), http.StatusCreated, http.StatusOK)
	anna.must(anna.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-02-04", "to": "2026-02-04", "kind": "halfVacation",
	}), http.StatusCreated, http.StatusOK)

	account := vacationIn(t, anna, "2026")

	if account.Entitlement != 28 || account.CarriedOver != 23 || account.Taken != 2.5 || account.Remaining != 48.5 {
		t.Errorf("2026 reads %+v, want 28 given, 23 carried, 2.5 taken and 48.5 left", account)
	}

	if len(account.Entitlements) != 1 || account.Entitlements[0].FromYear != 2025 {
		t.Errorf("the entitlements read %+v, want the one from 2025", account.Entitlements)
	}

	if theirs := vacationIn(t, bert, "2026"); theirs.Entitlement != 0 || len(theirs.Entitlements) != 0 {
		t.Errorf("a colleague's account reads %+v, want nothing of Anna's", theirs)
	}

	anna.must(anna.api(http.MethodDelete, "/me/vacation/2025", nil), http.StatusOK, http.StatusNoContent)

	if again := anna.api(http.MethodDelete, "/me/vacation/2025", nil); again.Status != http.StatusNotFound {
		t.Errorf("taking an entitlement back twice answered %d, want 404", again.Status)
	}
}

// What cannot be given is refused by the field it is in.
func TestLeaveThatCannotBeGivenIsRefused(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	for _, body := range []map[string]any{
		{"fromYear": 2026},
		{"fromYear": 2026, "days": 400},
		{"fromYear": 1800, "days": 30},
	} {
		if refused := worker.api(http.MethodPut, "/me/vacation", body); refused.Status != http.StatusBadRequest {
			t.Errorf("%v answered %d, want 400", body, refused.Status)
		}
	}
}

// An account's entitlements go with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsLeaveWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	worker.must(worker.api(http.MethodPut, "/me/vacation", map[string]any{"fromYear": 2026, "days": 30}), http.StatusOK)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "vacation_entitlements", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d entitlement(s) outlived the account they belonged to", left)
	}
}
