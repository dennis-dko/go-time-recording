//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// Statistics over your own time deliberately do not go through the project
// report. That one is keyed on a project id, needs reports:read - which both
// default roles are without, so only the built-in administrator can see what other
// people total up to - and cannot express "every project" or "no project" at all.
//
// So the thing worth proving is that an ordinary employee can read their own
// figures, and that the uncategorised hours are an answer rather than a gap.

// StatisticsOnTheWire mirrors the response.
type StatisticsOnTheWire struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	TotalHours float64 `json:"totalHours"`

	Days []struct {
		Date  string  `json:"date"`
		Hours float64 `json:"hours"`
	} `json:"days"`

	Projects []struct {
		ProjectID *uint   `json:"projectId"`
		Name      string  `json:"name"`
		Hours     float64 `json:"hours"`
	} `json:"projects"`

	Statuses []struct {
		Status string  `json:"status"`
		Hours  float64 `json:"hours"`
	} `json:"statuses"`
}

func ownStatistics(t *testing.T, c *client, query string) StatisticsOnTheWire {
	t.Helper()

	var stats StatisticsOnTheWire

	c.must(c.api(http.MethodGet, "/me/statistics"+query, nil), http.StatusOK).Data(t, &stats)

	return stats
}

// An employee has timesheets:read:own and not reports:read, which is exactly the
// account that could not have had a chart of its own week before.
func TestAnEmployeeCanReadTheirOwnStatistics(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Hanne", "email": "hanne@example.com",
		"role": "employee", "password": "hanne-password-1",
	}), http.StatusCreated, http.StatusOK)

	var shared projectResponse
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	hanne := a.newClient()
	hanne.signIn("hanne@example.com", "hanne-password-1")

	// Two entries on one project, one on none, on two different days.
	for _, entry := range []map[string]any{
		{"date": "2026-08-03", "durationHours": 2.5, "projectId": shared.ID},
		{"date": "2026-08-03", "durationHours": 1.25, "projectId": shared.ID},
		{"date": "2026-08-05", "durationHours": 3, "description": "not categorised yet"},
	} {
		hanne.must(hanne.api(http.MethodPost, "/timesheets", entry),
			http.StatusCreated, http.StatusOK)
	}

	// The project report is refused, which is the whole reason this endpoint
	// exists rather than a second use of that one.
	if got := hanne.api(http.MethodGet,
		path("/projects/", shared.ID)+"/report", nil).Status; got == http.StatusOK {
		t.Error("an employee could read a project report; this test no longer proves anything")
	}

	stats := ownStatistics(t, hanne, "?from=2026-08-01&to=2026-08-31")

	if stats.TotalHours != 6.75 {
		t.Errorf("the total is %v, want 6.75", stats.TotalHours)
	}

	// Both projects, and the bucket for the entries that have none.
	byName := map[string]float64{}
	var uncategorised float64

	for _, project := range stats.Projects {
		if project.ProjectID == nil {
			uncategorised = project.Hours

			continue
		}

		byName[project.Name] = project.Hours
	}

	if byName["Shared work"] != 3.75 {
		t.Errorf("the project totals %v hours, want 3.75", byName["Shared work"])
	}

	if uncategorised != 3 {
		t.Errorf("the uncategorised hours come to %v, want 3", uncategorised)
	}

	// Hours per day, and the days with nothing on them are there too - a chart
	// drawn only from the days that have entries shows a full week where there
	// were two working days.
	perDay := map[string]float64{}
	for _, day := range stats.Days {
		perDay[day.Date] = day.Hours
	}

	if perDay["2026-08-03"] != 3.75 {
		t.Errorf("3 August totals %v, want 3.75", perDay["2026-08-03"])
	}

	if perDay["2026-08-05"] != 3 {
		t.Errorf("5 August totals %v, want 3", perDay["2026-08-05"])
	}

	if _, present := perDay["2026-08-04"]; !present {
		t.Error("4 August is missing entirely; an empty day has to be a zero, not a gap")
	}

	if perDay["2026-08-04"] != 0 {
		t.Errorf("4 August has %v hours, want 0", perDay["2026-08-04"])
	}

	if len(stats.Days) != 31 {
		t.Errorf("the range covers %d days, want 31 for August", len(stats.Days))
	}
}

// Somebody else's hours are never in your own figures, whatever role you have.
func TestOwnStatisticsAreOnlyYourOwn(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Ilka", "email": "ilka@example.com",
		"role": "employee", "password": "ilka-password-1",
	}), http.StatusCreated, http.StatusOK)

	ilka := a.newClient()
	ilka.signIn("ilka@example.com", "ilka-password-1")

	ilka.must(ilka.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK)

	// The administrator holds every permission, including reading everyone's
	// entries - and still sees only their own here, because this endpoint is
	// keyed on the caller rather than filtered by one.
	if stats := ownStatistics(t, admin, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 0 {
		t.Errorf("the administrator's own statistics include %v hours of somebody else's",
			stats.TotalHours)
	}

	if stats := ownStatistics(t, ilka, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 4 {
		t.Errorf("Ilka's own statistics total %v, want 4", stats.TotalHours)
	}
}

// A rejected entry is not work anybody is counting, which is the reading the
// overtime balance already takes.
func TestRejectedTimeIsLeftOutOfTheTotals(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var entry timesheetResponse
	admin.must(admin.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 5,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	if stats := ownStatistics(t, admin, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 5 {
		t.Fatalf("the entry totals %v before being rejected, want 5", stats.TotalHours)
	}

	// open -> submitted -> rejected, which is the only way to reach it.
	admin.must(admin.api(http.MethodPut, path("/timesheets/", entry.ID),
		map[string]any{"status": "submitted"}), http.StatusOK)
	admin.must(admin.api(http.MethodPut, path("/timesheets/", entry.ID),
		map[string]any{"status": "rejected"}), http.StatusOK)

	if stats := ownStatistics(t, admin, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 0 {
		t.Errorf("a rejected entry still counts %v hours", stats.TotalHours)
	}
}

// A range the wrong way round is a mistake worth reporting rather than an empty
// chart to puzzle over.
func TestAnInvertedRangeIsRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if got := admin.api(http.MethodGet,
		"/me/statistics?from=2026-08-31&to=2026-08-01", nil).Status; got == http.StatusOK {
		t.Error("a range ending before it starts was accepted")
	}
}
