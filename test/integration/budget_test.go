//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type budgetedProject struct {
	ID          uint     `json:"id"`
	BudgetHours *float64 `json:"budgetHours"`
	BookedHours float64  `json:"bookedHours"`
}

// A project's budget is kept and read beside the hours booked on it, on every
// database, and taken off by a budget of nothing.
func TestAProjectsBudgetIsReadBesideItsHours(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	var made budgetedProject

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Portal", "startDate": "2026-08-01", "budgetHours": 10,
	}), http.StatusCreated, http.StatusOK).Data(t, &made)

	if made.BudgetHours == nil || *made.BudgetHours != 10 || made.BookedHours != 0 {
		t.Fatalf("a new project reads %+v, want a budget of 10 and nothing booked", made)
	}

	for _, day := range []string{"2026-08-03", "2026-08-04"} {
		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": day, "durationHours": 2.5, "projectId": made.ID,
		}), http.StatusCreated, http.StatusOK)
	}

	var listed listOf[budgetedProject]

	worker.must(worker.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 1 || listed.Items[0].BookedHours != 5 {
		t.Errorf("the list reads %+v, want the five hours booked", listed.Items)
	}

	var cleared budgetedProject

	worker.must(worker.api(http.MethodPut, path("/projects/", made.ID), map[string]any{"budgetHours": 0}),
		http.StatusOK).Data(t, &cleared)

	if cleared.BudgetHours != nil || cleared.BookedHours != 5 {
		t.Errorf("a budget of nothing reads %+v, want no budget and still five hours", cleared)
	}

	if refused := worker.api(http.MethodPut, path("/projects/", made.ID), map[string]any{"budgetHours": -1}); refused.Status != http.StatusBadRequest {
		t.Errorf("a budget of minus one answered %d, want 400", refused.Status)
	}
}
