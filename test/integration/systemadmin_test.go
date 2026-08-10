//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// The built-in administrator administers the installation and nothing else.
//
// Every installation has this account, and nobody chose to give it anything: it
// arrived with the software. So it is the one account that must not be able to
// read or edit what other people recorded - an administrator restoring a backup
// or repointing the directory has no business in a colleague's week.
//
// Enforced by the same per-endpoint checks that refuse an employee, which is what
// these tests go through. A guarantee that only holds in the interface is not a
// guarantee; the interface hides what it cannot do, and a token client does not.

// employeeWithTime returns an employee and one entry of theirs, booked by
// themselves - the administrator could not book it for them.
func employeeWithTime(t *testing.T, a *app, admin *client) (*client, timesheetResponse) {
	t.Helper()

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Nadja", "email": "nadja@example.com",
		"role": "employee", "password": "nadja-password-1",
	}), http.StatusCreated, http.StatusOK)

	employee := a.newClient()
	employee.signIn("nadja@example.com", "nadja-password-1")

	var entry timesheetResponse
	employee.must(employee.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4, "description": "her own work",
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	return employee, entry
}

func TestTheAdministratorCannotReachSomebodyElsesTime(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	owner, entry := employeeWithTime(t, a, admin)

	// The listing is refused outright now, where it used to be scoped to the caller:
	// the administrator had its own entries to read and somebody else's were simply
	// not among them. It has none, because it does not record time - so there is
	// nothing to scope and the right to read a list at all is gone.
	if got := admin.api(http.MethodGet, "/timesheets", nil).Status; got == http.StatusOK {
		t.Error("the administrator read a list of time entries; it records none")
	}

	// Asking for hers by name is refused the same way.
	if got := admin.api(http.MethodGet,
		path("/timesheets?userId=", entry.UserID), nil).Status; got == http.StatusOK {
		t.Error("the administrator read another user's entries by filtering for them")
	}

	// Reading, changing and moving one, each refused on its own - they are
	// different rights and a single check would not cover the others.
	for _, attempt := range []struct {
		what   string
		method string
		route  string
		body   map[string]any
	}{
		{"read one entry", http.MethodGet, path("/timesheets/", entry.ID), nil},
		{"change the hours", http.MethodPut, path("/timesheets/", entry.ID),
			map[string]any{"durationHours": 9}},
		{"transfer it", http.MethodPost, path("/timesheets/", entry.ID) + "/transfer",
			map[string]any{"projectId": 1}},
	} {
		if got := admin.api(attempt.method, attempt.route, attempt.body).Status; got == http.StatusOK {
			t.Errorf("the administrator could %s of somebody else's", attempt.what)
		}
	}

	// The team-wide overtime route was here, and it is gone rather than refused:
	// comparing colleagues is the one thing this arrangement says nobody does.
	if got := admin.api(http.MethodGet, "/overtime", nil).Status; got == http.StatusOK {
		t.Error("the administrator read the overtime balance of every account")
	}

	// The entry is untouched by all of that, read back by the person it belongs to.
	var after timesheetResponse

	owner.must(owner.api(http.MethodGet, path("/timesheets/", entry.ID), nil),
		http.StatusOK).Data(t, &after)

	if after.DurationHours != 4 {
		t.Errorf("the entry is now %v hours; it was 4", after.DurationHours)
	}
}

// The projects everybody books against belong to whoever runs the work.
func TestTheAdministratorDoesNotKeepTheSharedProjects(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	other := a.signInAsUser(admin, "Momo", "momo@example.com")

	var shared projectResponse
	other.must(other.api(http.MethodPost, "/projects", map[string]any{
		"name": "Momo's work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	// It may not make one at all. This checked twice before that it could not make a
	// shared project, and then that it could make its own - a project is somewhere to
	// put hours, and the administrator records none.
	if got := admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Mine to make", "startDate": "2026-08-01",
	}).Status; got == http.StatusCreated || got == http.StatusOK {
		t.Error("the administrator created a project; it has no hours to put in one")
	}

	for _, attempt := range []struct {
		what   string
		method string
		route  string
		body   map[string]any
	}{
		{"rename it", http.MethodPut, path("/projects/", shared.ID),
			map[string]any{"name": "Renamed", "startDate": "2026-08-01"}},
		{"archive it", http.MethodPost, path("/projects/", shared.ID) + "/archive", nil},
		{"delete it", http.MethodDelete, path("/projects/", shared.ID), nil},
	} {
		got := admin.api(attempt.method, attempt.route, attempt.body).Status
		if got == http.StatusOK || got == http.StatusNoContent {
			t.Errorf("the administrator could %s", attempt.what)
		}
	}

	// A report is refused too, and the same way: as though the project were not there.
	// Confirming the id exists would be a way to find out what colleagues have.
	if got := admin.api(http.MethodGet,
		path("/projects/", shared.ID)+"/report", nil).Status; got == http.StatusOK {
		t.Errorf("the administrator read a colleague's project report: %d", got)
	}

	// And the owner still can, so none of this passes by reports being broken.
	other.must(other.api(http.MethodGet, path("/projects/", shared.ID)+"/report", nil),
		http.StatusOK)

	// Reading the list is still allowed: time has to be bookable against
	// something, and that list is what every employee sees anyway.
	// The list is refused as well. This checked twice before: first that the shared
	// project was visible so there was something to book against, then that only its
	// own was. It has none and books nothing, so the right to read the list is gone
	// with the rest.
	if got := admin.api(http.MethodGet, "/projects", nil).Status; got == http.StatusOK {
		t.Error("the administrator read the project list; it keeps no projects")
	}
}

// The other half: what was taken away has to be exactly the work, or an installation
// is left unadministrable.
//
// It was called "still administers and still works". It does not work here - the
// account exists on every installation before anybody has chosen anything, so it is
// how you get in rather than somebody's working day. Whoever does work here has an
// account of their own, and the role below is for the person who does both.
func TestTheAdministratorStillAdministersAndNothingElse(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Accounts and roles.
	var created struct {
		ID uint `json:"id"`
	}

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Olaf", "email": "olaf@example.com",
		"role": "employee", "password": "olaf-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/roles", map[string]any{
		"name": "auditor", "description": "reads the reports",
		"permissions": []string{"reports:read:own", "timesheets:read:all"},
	}), http.StatusCreated, http.StatusOK)

	// Somebody else's working times: not any more. A daily target is a time figure,
	// everything to do with time belongs to the person it is about, and this account
	// cannot read the entries, the balance or the figures those numbers produce -
	// setting a cause whose effect is invisible to you is not administering anything.
	if got := admin.api(http.MethodPut, path("/users/", created.ID, "/working-times"),
		map[string]any{"dailyTargetHours": 7}).Status; got != http.StatusForbidden {
		t.Errorf("the administrator set somebody else's working times: %d, want 403", got)
	}

	// Not even its own, which is the difference from before: it has no working day for
	// a daily target to be about.
	var self struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &self)

	if got := admin.api(http.MethodPut, path("/users/", self.User.ID, "/working-times"),
		map[string]any{"dailyTargetHours": 7}).Status; got != http.StatusForbidden {
		t.Errorf("the administrator set working times for itself: %d, want 403", got)
	}

	// The installation itself.
	admin.must(admin.api(http.MethodGet, "/setup", nil), http.StatusOK)
	admin.must(admin.api(http.MethodPut, "/settings/operational",
		map[string]any{"maxDailyHours": 10}), http.StatusOK)

	// And no working day of its own. Every one of these was here as something it
	// could do "like anybody who works here", and that was the wrong shape: the
	// account exists before anybody has chosen anything.
	for _, attempt := range []struct {
		what   string
		method string
		route  string
		body   map[string]any
	}{
		{"create a project", http.MethodPost, "/projects",
			map[string]any{"name": "Administration", "startDate": "2026-08-01"}},
		{"book time", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-03", "durationHours": 2}},
		{"read its own figures", http.MethodGet,
			"/me/statistics?from=2026-08-01&to=2026-08-31", nil},
		{"read its own overtime", http.MethodGet,
			path("/users/", self.User.ID, "/overtime"), nil},
	} {
		got := admin.api(attempt.method, attempt.route, attempt.body).Status
		if got == http.StatusOK || got == http.StatusCreated {
			t.Errorf("the administrator could %s; it does not record time", attempt.what)
		}
	}

	// What replaced it, and the reason none of the above is a loss: an account can be
	// given both jobs, and the administrator is who hands that out. Somebody who works
	// here and also administers keeps their own hours and administers the accounts -
	// which is the whole arrangement said in one test.
	both := a.signInAsWorkingAdmin(admin, "Bothe", "bothe@example.com")

	var theirs projectResponse
	both.must(both.api(http.MethodPost, "/projects", map[string]any{
		"name": "Both jobs", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &theirs)

	both.must(both.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2, "projectId": theirs.ID,
	}), http.StatusCreated, http.StatusOK)

	both.must(both.api(http.MethodGet, "/me/statistics?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK)

	// And the administration, which is what makes it the combined role rather than an
	// ordinary account.
	both.must(both.api(http.MethodPost, "/users", map[string]any{
		"name": "Hired", "email": "hired@example.com",
		"role": "employee", "password": "hired-password-1",
	}), http.StatusCreated, http.StatusOK)
}

// What the guarantee is, exactly.
//
// It is the seeded default, not a wall: whoever may manage roles may widen the role
// they hold, and taking that away would take role administration with it.
//
// Recorded as a test because the difference matters to anybody relying on it: the
// administrator cannot reach somebody else's hours by accident or by pointing a
// token at the API, and can reach them by deliberately granting the right to
// itself, where the role screen shows it holding it.
func TestTheSeparationIsTheDefaultRatherThanAWall(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	_, entry := employeeWithTime(t, a, admin)

	if got := admin.api(http.MethodGet, path("/timesheets/", entry.ID), nil).Status; got == http.StatusOK {
		t.Fatal("the administrator could read somebody else's entry before granting itself anything")
	}

	var roles listOf[struct {
		ID          uint     `json:"id"`
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}]

	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	var adminRole uint

	var held []string

	for _, role := range roles.Items {
		if role.Name == "admin" {
			adminRole = role.ID
			held = role.Permissions
		}
	}

	if adminRole == 0 {
		t.Fatal("there is no admin role")
	}

	// Every permission the seed leaves out is one the administrator does not hold.
	//
	// reports:read:own is not among them: its own figures are its own, like anybody
	// else's. What it must not have is any right over somebody else's work, and
	// reading everybody's time is the one right that grants that.
	for _, permission := range []string{
		"timesheets:read:all", "timesheets:write:all",
		// Gone from the application entirely; listed so a reintroduction is noticed.
		"timesheets:approve",
		"timesheets:transfer",
	} {
		for _, has := range held {
			if has == permission {
				t.Errorf("the seeded admin role holds %q", permission)
			}
		}
	}

	admin.must(admin.api(http.MethodPut, path("/roles/", adminRole), map[string]any{
		"permissions": append(held, "timesheets:read:all"),
	}), http.StatusOK)

	// The session carries the permissions, so a new one is needed to pick the
	// grant up - which is itself worth knowing.
	regranted := a.newClient()
	regranted.signIn(adminEmail, "a-much-better-password")

	regranted.must(regranted.api(http.MethodGet, path("/timesheets/", entry.ID), nil),
		http.StatusOK)
}
