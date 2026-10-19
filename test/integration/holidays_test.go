//go:build integration

package integration

import (
	"net/http"
	"testing"
)

type myHolidaysOnTheWire struct {
	Region          string   `json:"region"`
	InstanceRegion  string   `json:"instanceRegion"`
	EffectiveRegion string   `json:"effectiveRegion"`
	Regions         []string `json:"regions"`
}

func holidaysOf(t *testing.T, c *client) myHolidaysOnTheWire {
	t.Helper()

	var answer myHolidaysOnTheWire

	c.must(c.api(http.MethodGet, "/me/holidays", nil), http.StatusOK).Data(t, &answer)

	return answer
}

// An installation keeps no holidays until somebody chooses a state. Once it has
// one, everybody following it keeps that state's: the days name them, and leave
// over Whit week is four days of it.
func TestTheInstallationsHolidaysAreEverybodys(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	if fresh := holidaysOf(t, worker); fresh.EffectiveRegion != "" || len(fresh.Regions) != 16 {
		t.Errorf("a fresh installation reads %+v, want no holidays and the sixteen states to choose from", fresh)
	}

	admin.must(admin.api(http.MethodPut, "/settings/holidays", map[string]any{"region": "BY"}), http.StatusOK)

	if mine := holidaysOf(t, worker); mine.Region != "" || mine.EffectiveRegion != "BY" {
		t.Errorf("following a Bavarian installation reads %+v, want Bavaria in effect", mine)
	}

	var week daysOnTheWire

	// 25 May 2026 is Whit Monday.
	worker.must(worker.api(http.MethodGet, "/me/days?from=2026-05-25&to=2026-05-31", nil),
		http.StatusOK).Data(t, &week)

	if week.Days[0].Holiday != "whitMonday" || week.Days[1].Holiday != "" {
		t.Errorf("Whit week reads %+v, want the Monday named and the Tuesday not", week.Days[:2])
	}

	var recorded listOf[absenceOnTheWire]

	worker.must(worker.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-05-25", "to": "2026-05-29", "kind": "vacation",
	}), http.StatusCreated, http.StatusOK).Data(t, &recorded)

	if recorded.TotalCount != 4 {
		t.Errorf("Whit week was recorded as %d days of leave, want the four that are not Whit Monday", recorded.TotalCount)
	}
}

// A person's own choice wins: none at all, or back to following the
// installation.
func TestAPersonChoosesTheirOwnHolidays(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	admin.must(admin.api(http.MethodPut, "/settings/holidays", map[string]any{"region": "BY"}), http.StatusOK)
	worker.must(worker.api(http.MethodPut, "/me/holidays", map[string]any{"region": "none"}), http.StatusOK)

	if mine := holidaysOf(t, worker); mine.Region != "none" || mine.EffectiveRegion != "" || mine.InstanceRegion != "BY" {
		t.Errorf("keeping none in a Bavarian installation reads %+v", mine)
	}

	worker.must(worker.api(http.MethodPut, "/me/holidays", map[string]any{"region": ""}), http.StatusOK)

	if mine := holidaysOf(t, worker); mine.EffectiveRegion != "BY" {
		t.Errorf("following the installation again reads %+v, want Bavaria", mine)
	}
}

// What cannot be chosen is refused: a state that does not exist, "none" for the
// installation - which says none by being empty - and the installation's
// holidays by somebody who does not administer it.
func TestAHolidayRegionThatCannotBeChosenIsRefused(t *testing.T) {
	t.Parallel()

	_, admin, worker := startWithWorker(t)

	for _, refused := range []response{
		worker.api(http.MethodPut, "/me/holidays", map[string]any{"region": "XX"}),
		admin.api(http.MethodPut, "/settings/holidays", map[string]any{"region": "none"}),
	} {
		if refused.Status != http.StatusBadRequest {
			t.Errorf("an unknown region answered %d, want 400", refused.Status)
		}
	}

	if refused := worker.api(http.MethodPut, "/settings/holidays", map[string]any{"region": "BY"}); refused.Status != http.StatusForbidden {
		t.Errorf("somebody who does not administer set the installation's holidays: %d, want 403", refused.Status)
	}
}
