//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// Nobody sees what anybody else has.
//
// A sys admin exists for configuration, everybody manages their own, and no screen
// adds up somebody else's hours. Whether a caller may see another person's recorded
// time is one question, and timesheets:read:all is the one right that answers it -
// for a list, for an export, for a project total and for an overtime balance alike.
// There used to be a second right for the totals, held by the role that reviewed
// other people's work; when that role went, that right was left gating a screen
// nobody could reach.

// A project total covers the caller's own hours and nobody else's.
func TestAProjectTotalCoversOnlyYourOwnHours(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")
	bert := a.signInAsUser(admin, "Bert", "bert@example.com")

	var shared projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	// Both book on it: three hours each, on the same day.
	for _, who := range []*client{anna, bert} {
		who.must(who.api(http.MethodPost, "/timesheets", map[string]any{
			"date": "2026-08-04", "durationHours": 3, "projectId": shared.ID,
		}), http.StatusCreated, http.StatusOK)
	}

	report := anna.must(anna.api(http.MethodGet,
		path("/projects/", shared.ID)+"/report?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK)

	var out struct {
		Total   float64 `json:"totalHours"`
		Entries []struct {
			UserID uint    `json:"userId"`
			Hours  float64 `json:"hours"`
		} `json:"entries"`
	}

	report.Data(t, &out)

	// Anna's three, not the six that were booked.
	if len(out.Entries) != 1 {
		t.Fatalf("the total breaks down into %d people, want 1: %+v", len(out.Entries), out.Entries)
	}

	if out.Entries[0].Hours != 3 {
		t.Errorf("the total is %v hours, want 3 - the caller's own", out.Entries[0].Hours)
	}

	if out.Total != 3 {
		t.Errorf("totalHours is %v, want 3", out.Total)
	}
}

// The report is reachable at all, which it was not.
//
// It was gated on a right no seeded role held, so the screen was hidden and the
// endpoint refused everybody - on every installation there is.
func TestTheProjectReportIsReachableByAnOrdinaryAccount(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	var project projectResponse
	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	anna.must(anna.api(http.MethodGet, path("/projects/", project.ID)+"/report", nil),
		http.StatusOK)

	// And by the administrator, for its own hours: it configures the installation
	// and records its own time like anybody else.
	admin.must(admin.api(http.MethodGet, path("/projects/", project.ID)+"/report", nil),
		http.StatusOK)
}

// Somebody else's overtime balance is refused, to everybody a fresh install has.
//
// A balance is that person's recorded time, totalled, so it takes the same right as
// reading their entries - which no default role holds, including the administrator's.
func TestSomebodyElsesOvertimeIsRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")

	listed := admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)

	var people struct {
		Items []struct {
			ID    uint   `json:"id"`
			Email string `json:"email"`
		} `json:"items"`
	}

	listed.Data(t, &people)

	var annaID uint

	for _, person := range people.Items {
		if person.Email == "anna@example.com" {
			annaID = person.ID
		}
	}

	if annaID == 0 {
		t.Fatal("Anna is not in the account list")
	}

	// Not even the built-in administrator, which is the point of the arrangement.
	if got := admin.api(http.MethodGet, path("/users/", annaID)+"/overtime", nil).Status; got !=
		http.StatusForbidden {
		t.Errorf("the administrator reading somebody else's balance answered %d, want 403", got)
	}

	// Anna's own is hers to see.
	anna.must(anna.api(http.MethodGet, path("/users/", annaID)+"/overtime", nil),
		http.StatusOK)
}

// The team overview is gone, not merely hidden.
//
// It existed to compare colleagues, which is the one thing this arrangement says
// nobody does. A route left in place for a screen nobody may open is a route
// somebody finds later.
func TestThereIsNoTeamWideOvertimeEndpoint(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if got := admin.api(http.MethodGet, "/overtime", nil).Status; got != http.StatusNotFound {
		t.Errorf("GET /overtime answered %d, want 404 - the team-wide balance was removed", got)
	}
}
