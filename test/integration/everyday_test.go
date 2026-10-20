//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// workingTimes saves somebody's working times, the counting day among them.
func workingTimes(t *testing.T, c *client, body map[string]any) userResponse {
	t.Helper()

	var me struct {
		User userResponse `json:"user"`
	}

	c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	var saved userResponse

	c.must(c.api(http.MethodPut, path("/users/", me.User.ID, "/working-times"), body), http.StatusOK).Data(t, &saved)

	return saved
}

// From the day chosen, every working day of Whit week counts on every database:
// Whit Monday asks nothing and its two booked hours are overtime, a day of leave
// is not counted, the two days nobody booked are eight hours short each, and a
// day off against overtime takes the whole day off the balance.
func TestFromTheChosenDayEveryWorkingDayCounts(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	admin.must(admin.api(http.MethodPut, "/settings/holidays", map[string]any{"region": "BY"}), http.StatusOK)

	if saved := workingTimes(t, worker, map[string]any{"everyDayCountsFrom": "2026-05-25"}); saved.EveryDayCountsFrom == nil ||
		*saved.EveryDayCountsFrom != "2026-05-25" {
		t.Fatalf("the day was saved as %v, want 2026-05-25", saved.EveryDayCountsFrom)
	}

	for day, kind := range map[string]string{"2026-05-26": "vacation", "2026-05-29": "flexDay"} {
		worker.must(worker.api(http.MethodPost, "/me/absences", map[string]any{
			"from": day, "to": day, "kind": kind,
		}), http.StatusCreated, http.StatusOK)
	}

	bookHours(t, worker, "2026-05-25", 2)

	total, days := balanceOf(t, worker, "2026-05-25", "2026-05-31")

	for day, want := range map[string]float64{
		"2026-05-25": 2, "2026-05-27": -8, "2026-05-28": -8, "2026-05-29": -8,
	} {
		if got, counted := days[day]; !counted || got != want {
			t.Errorf("%s reads %.2f (counted: %v), want %.2f", day, got, counted, want)
		}
	}

	if _, counted := days["2026-05-26"]; counted {
		t.Error("a day of leave counts")
	}

	if total != -22 {
		t.Errorf("Whit week reads %.2f, want -22", total)
	}
}

// Taking the day back with an empty field puts back the rule from before: only
// the days booked on count.
func TestTakingTheDayBackCountsOnlyTheBookedDays(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	workingTimes(t, worker, map[string]any{"everyDayCountsFrom": "2026-08-03"})
	bookHours(t, worker, "2026-08-03", 8)

	if total, days := balanceOf(t, worker, "2026-08-03", "2026-08-07"); total != -32 || len(days) != 5 {
		t.Errorf("a week with one booked day reads %.2f over %d day(s), want -32 over five", total, len(days))
	}

	if saved := workingTimes(t, worker, map[string]any{"everyDayCountsFrom": ""}); saved.EveryDayCountsFrom != nil {
		t.Fatalf("an empty field left the day at %v", *saved.EveryDayCountsFrom)
	}

	if total, days := balanceOf(t, worker, "2026-08-03", "2026-08-07"); total != 0 || len(days) != 1 {
		t.Errorf("without the day the week reads %.2f over %d day(s), want 0 over the one booked", total, len(days))
	}

	// Saving the other working times leaves the day as it is: absent is not empty.
	workingTimes(t, worker, map[string]any{"everyDayCountsFrom": "2026-08-03"})

	if saved := workingTimes(t, worker, map[string]any{"dailyTargetHours": 7}); saved.EveryDayCountsFrom == nil {
		t.Error("saving the daily target took the day off as well")
	}
}
