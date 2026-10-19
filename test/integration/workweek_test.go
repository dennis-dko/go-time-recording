//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// The weeks a balance is measured against, over HTTP: planned, read back and
// taken back, the balance following them, and an account's plans going with it.
// August 2026: the 3rd and the 10th are Mondays, the 7th a Friday, the 8th a
// Saturday.

type plannedWeeks struct {
	Items []struct {
		ValidFrom string     `json:"validFrom"`
		Hours     [7]float64 `json:"hours"`
	} `json:"items"`
	Before [7]float64 `json:"before"`
}

func plansOf(t *testing.T, c *client) plannedWeeks {
	t.Helper()

	var weeks plannedWeeks

	c.must(c.api(http.MethodGet, "/me/schedules", nil), http.StatusOK).Data(t, &weeks)

	return weeks
}

func balanceOf(t *testing.T, c *client, from, to string) (total float64, days map[string]float64) {
	t.Helper()

	var me struct {
		User userResponse `json:"user"`
	}

	c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	var balance struct {
		TotalBalance float64 `json:"totalBalance"`
		Days         []struct {
			Date    string  `json:"date"`
			Balance float64 `json:"balance"`
		} `json:"days"`
	}

	c.must(c.api(http.MethodGet, path("/users/", me.User.ID, "/overtime?from=", from, "&to=", to), nil),
		http.StatusOK).Data(t, &balance)

	days = map[string]float64{}
	for _, d := range balance.Days {
		days[d.Date[:10]] = d.Balance
	}

	return balance.TotalBalance, days
}

func bookHours(t *testing.T, c *client, date string, hours float64) {
	t.Helper()

	c.must(c.api(http.MethodPost, "/timesheets", map[string]any{
		"date": date, "durationHours": hours,
	}), http.StatusCreated, http.StatusOK)
}

// A plan is saved, read back with the week before it, and taken back.
func TestAWeeklyPlanIsSavedReadAndRemoved(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	if weeks := plansOf(t, worker); len(weeks.Items) != 0 || weeks.Before != [7]float64{8, 8, 8, 8, 8, 0, 0} {
		t.Fatalf("a fresh account reads %+v, want no plans and the default week before", weeks)
	}

	worker.must(worker.api(http.MethodPost, "/me/schedules", map[string]any{
		"validFrom": "2026-08-03", "hours": []float64{8, 8, 8, 8, 6, 0, 0},
	}), http.StatusOK, http.StatusCreated)

	weeks := plansOf(t, worker)
	if len(weeks.Items) != 1 || weeks.Items[0].ValidFrom != "2026-08-03" ||
		weeks.Items[0].Hours != [7]float64{8, 8, 8, 8, 6, 0, 0} {
		t.Fatalf("the plan reads back as %+v", weeks.Items)
	}

	worker.must(worker.api(http.MethodDelete, "/me/schedules/2026-08-03", nil), http.StatusOK, http.StatusNoContent)

	if weeks := plansOf(t, worker); len(weeks.Items) != 0 {
		t.Errorf("the removed plan still reads back: %+v", weeks.Items)
	}

	if got := worker.api(http.MethodDelete, "/me/schedules/2026-08-03", nil).Status; got != http.StatusNotFound {
		t.Errorf("removing it again answered %d, want 404", got)
	}
}

// The balance follows the week: a Saturday is overtime without a plan, and a
// Friday planned at six is met by six.
func TestTheBalanceFollowsTheWeeklyPlan(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	bookHours(t, worker, "2026-08-08", 4) // a Saturday, no plan

	if total, days := balanceOf(t, worker, "2026-08-08", "2026-08-08"); total != 4 || days["2026-08-08"] != 4 {
		t.Errorf("four hours on a Saturday stand at %+.2f, want +4", total)
	}

	worker.must(worker.api(http.MethodPost, "/me/schedules", map[string]any{
		"validFrom": "2026-08-10", "hours": []float64{8, 8, 8, 8, 6, 0, 0},
	}), http.StatusOK, http.StatusCreated)

	bookHours(t, worker, "2026-08-07", 6) // the Friday before the plan
	bookHours(t, worker, "2026-08-14", 6) // a Friday under it

	_, days := balanceOf(t, worker, "2026-08-03", "2026-08-16")

	if days["2026-08-07"] != -2 {
		t.Errorf("the Friday before the plan stands at %+.2f, want -2", days["2026-08-07"])
	}

	if days["2026-08-14"] != 0 {
		t.Errorf("the Friday under the plan stands at %+.2f, want 0", days["2026-08-14"])
	}
}

// Seven figures, Monday first, or the plan is refused: six would quietly be a
// week with a day missing.
func TestAPlanNeedsAFigureForEveryDay(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	refused := worker.api(http.MethodPost, "/me/schedules", map[string]any{
		"validFrom": "2026-08-03", "hours": []float64{8, 8, 8, 8, 8, 0},
	})

	if refused.Status != http.StatusBadRequest {
		t.Errorf("a six-day plan answered %d, want 400: %s", refused.Status, refused.Body)
	}
}

// An account's plans go with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsPlansWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	worker.must(worker.api(http.MethodPost, "/me/schedules", map[string]any{
		"validFrom": "2026-08-03", "hours": []float64{8, 8, 8, 8, 6, 0, 0},
	}), http.StatusOK, http.StatusCreated)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "work_schedules", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d plan(s) outlived the account they belonged to", left)
	}
}
