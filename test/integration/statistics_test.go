//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// Statistics over your own time deliberately do not go through the project report.
// That one is keyed on a project id and cannot express "every project" or "no
// project" at all - and it totals only the caller's own hours, because nobody sees
// what anybody else has.
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
}

func ownStatistics(t *testing.T, c *client, query string) StatisticsOnTheWire {
	t.Helper()

	var stats StatisticsOnTheWire

	c.must(c.api(http.MethodGet, "/me/statistics"+query, nil), http.StatusOK).Data(t, &stats)

	return stats
}

// An employee reads their own time and nobody else's, which is exactly the account
// that could not have had a chart of its own week before.
func TestAnEmployeeCanReadTheirOwnStatistics(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Hanne", "email": "hanne@example.com",
		"role": "employee", "password": "hanne-password-1",
	}), http.StatusCreated, http.StatusOK)

	hanne := a.newClient()
	hanne.signIn("hanne@example.com", "hanne-password-1")

	// Her own project. It used to come from another account, as the shared kind did;
	// a project belongs to one person now, so hers is the only one she can book on.
	var shared projectResponse
	hanne.must(hanne.api(http.MethodPost, "/projects", map[string]any{
		"name": "Her work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	// Two entries on one project, one on none, on two different days.
	for _, entry := range []map[string]any{
		{"date": "2026-08-03", "durationHours": 2.5, "projectId": shared.ID},
		{"date": "2026-08-03", "durationHours": 1.25, "projectId": shared.ID},
		{"date": "2026-08-05", "durationHours": 3, "description": "not categorised yet"},
	} {
		hanne.must(hanne.api(http.MethodPost, "/timesheets", entry),
			http.StatusCreated, http.StatusOK)
	}

	// The project report is open to them now, and covers their own hours - it used to
	// be refused, because it broke down what every colleague had booked and was gated
	// on a right no role held. This endpoint still earns its place: the report is keyed
	// on a project id and cannot express "every project" or "no project", which is
	// exactly what the figures below are about.
	hanne.must(hanne.api(http.MethodGet, path("/projects/", shared.ID)+"/report", nil),
		http.StatusOK)

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

	if byName["Her work"] != 3.75 {
		t.Errorf("the project totals %v hours, want 3.75", byName["Her work"])
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
	a, admin, wera := startWithWorker(t)

	wera.must(wera.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK)

	// The built-in administrator used to be the caller that made this point, on the
	// grounds that it held every permission there was. It holds nothing over time now
	// - it administers the installation and the accounts - so it cannot even ask this
	// question, and the claim moves to the role that genuinely can see everybody:
	// somebody who may read all the entries there are.
	ilka := a.signInAsAuditor(admin, "Ilka", "ilka@example.com")

	// And that account still sees only its own hours here, because this endpoint is
	// keyed on the caller rather than filtered by one.
	if stats := ownStatistics(t, ilka, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 0 {
		t.Errorf("the statistics of somebody who may read everybody's entries include %v hours of somebody else's",
			stats.TotalHours)
	}

	if stats := ownStatistics(t, wera, "?from=2026-08-01&to=2026-08-31"); stats.TotalHours != 4 {
		t.Errorf("Wera's own statistics total %v, want 4", stats.TotalHours)
	}
}

// A range the wrong way round is a mistake worth reporting rather than an empty
// chart to puzzle over.
func TestAnInvertedRangeIsRefused(t *testing.T) {
	_, _, worker := startWithWorker(t)

	if got := worker.api(http.MethodGet,
		"/me/statistics?from=2026-08-31&to=2026-08-01", nil).Status; got == http.StatusOK {
		t.Error("a range ending before it starts was accepted")
	}
}
