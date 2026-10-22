//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

type notices struct {
	Balance *struct {
		Hours      float64  `json:"hours"`
		Since      *string  `json:"since"`
		UpperBound *float64 `json:"upperBound"`
		LowerBound *float64 `json:"lowerBound"`
		Outside    string   `json:"outside"`
	} `json:"balance"`
}

// The bounds of a running balance are kept on every database and read back with
// the balance they bound - set, crossed and cleared again - and an account that
// records no time is answered with nothing rather than refused, because the
// screen asks on every load.
func TestTheBoundsOfARunningBalanceAreKeptAndReadBack(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	// Two Fridays long past: 1.5 and 1 hours over the default eight.
	for day, hours := range map[string]float64{"2026-07-03": 9.5, "2026-07-10": 9} {
		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": day, "durationHours": hours,
		}), http.StatusCreated, http.StatusOK)
	}

	var bounded notices

	worker.must(worker.api(http.MethodPut, "/me/balance-limits", map[string]any{
		"upperBound": 2, "lowerBound": -20,
	}), http.StatusOK).Data(t, &bounded)

	balance := bounded.Balance
	if balance == nil || balance.Hours != 2.5 || balance.Since == nil || *balance.Since != "2026-07-03" {
		t.Fatalf("the running balance reads %+v, want 2.5 h since 3 July", balance)
	}

	if balance.UpperBound == nil || *balance.UpperBound != 2 || balance.LowerBound == nil ||
		*balance.LowerBound != -20 || balance.Outside != "above" {
		t.Errorf("the bounds read back as %+v, want 2 and -20 with the balance above", balance)
	}

	var cleared notices

	worker.must(worker.api(http.MethodPut, "/me/balance-limits", map[string]any{"upperBound": nil}),
		http.StatusOK).Data(t, &cleared)

	if b := cleared.Balance; b == nil || b.UpperBound != nil || b.LowerBound != nil || b.Outside != "" {
		t.Errorf("after clearing both bounds the balance reads %+v, want no bound and nothing outside", b)
	}

	refused := worker.api(http.MethodPut, "/me/balance-limits", map[string]any{"upperBound": -1})
	if refused.Status != http.StatusBadRequest || !strings.Contains(string(refused.Body), "upperBound") {
		t.Errorf("an upper bound below zero answered %d: %s", refused.Status, refused.Body)
	}

	var nothing map[string]any

	admin.must(admin.api(http.MethodGet, "/me/notices", nil), http.StatusOK).Data(t, &nothing)

	if len(nothing) != 0 {
		t.Errorf("the built-in administrator, who records no time, is told %v", nothing)
	}
}
