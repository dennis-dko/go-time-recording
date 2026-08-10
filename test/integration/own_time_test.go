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

// A project total covers the caller's own hours, and there is no way for anybody
// else's to be in it.
//
// This case used to have two colleagues booking on one project and checked that the
// total showed only one of them. They cannot: a project belongs to one person, so two
// people working on the same thing keep a project each, with the same name if they
// like. What is checked now is both halves of that - each total holds its owner's hours
// and the other project is not reachable at all.
func TestAProjectTotalCoversOnlyYourOwnHours(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	anna := a.signInAsUser(admin, "Anna", "anna@example.com")
	bert := a.signInAsUser(admin, "Bert", "bert@example.com")

	// One name, two projects, one each.
	var hers, his projectResponse

	anna.must(anna.api(http.MethodPost, "/projects", map[string]any{
		"name": "Roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &hers)

	bert.must(bert.api(http.MethodPost, "/projects", map[string]any{
		"name": "Roof", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &his)

	if hers.ID == his.ID {
		t.Fatal("both accounts were given the same project")
	}

	// Three hours each, on the same day, each on their own.
	for _, who := range []struct {
		client  *client
		project uint
	}{{anna, hers.ID}, {bert, his.ID}} {
		who.client.must(who.client.api(http.MethodPost, "/timesheets", map[string]any{
			"date": "2026-08-04", "durationHours": 3, "projectId": who.project,
		}), http.StatusCreated, http.StatusOK)
	}

	// Bert's project is not even reachable for Anna, which is the stronger half: the
	// total cannot hold somebody else's hours because the project cannot be shared.
	if got := anna.api(http.MethodGet,
		path("/projects/", his.ID)+"/report", nil).Status; got != http.StatusNotFound {
		t.Errorf("a colleague's project report answered %d, want 404", got)
	}

	report := anna.must(anna.api(http.MethodGet,
		path("/projects/", hers.ID)+"/report?from=2026-08-01&to=2026-08-31", nil),
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

	// And by the administrator, for a project of its own: it configures the
	// installation and records its own time like anybody else. Anna's is not its
	// business, which the case above covers.
	var mine projectResponse
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Administration", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &mine)

	admin.must(admin.api(http.MethodGet, path("/projects/", mine.ID)+"/report", nil),
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
