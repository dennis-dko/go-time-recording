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

	// The listing is scoped to the caller rather than refused outright: the
	// administrator has their own entries to read, and somebody else's are simply
	// not among them.
	var listed listOf[timesheetResponse]
	admin.must(admin.api(http.MethodGet, "/timesheets", nil), http.StatusOK).Data(t, &listed)

	for _, item := range listed.Items {
		if item.ID == entry.ID {
			t.Error("the administrator's own listing contains somebody else's entry")
		}
	}

	// And asking for hers by name is refused rather than quietly answered with
	// the administrator's own.
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

	// Everybody's overtime balance in one list is the same reading by another
	// route, and hangs on the report right the administrator no longer holds.
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
		"name": "Everyone's work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	if got := admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Mine to make", "startDate": "2026-08-01",
	}).Status; got == http.StatusCreated || got == http.StatusOK {
		t.Error("the administrator created a shared project")
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

	// The report is a different matter now, and worth stating rather than leaving
	// out: the administrator may open it, and what it shows is its own hours. It used
	// to break down what every colleague had booked, gated on a right no role held -
	// so this list refused it for the right reason by accident. Opening a report that
	// is empty because you booked nothing reveals nothing about anybody.
	report := admin.must(admin.api(http.MethodGet,
		path("/projects/", shared.ID)+"/report", nil), http.StatusOK)

	var totals struct {
		Entries []struct {
			UserID uint    `json:"userId"`
			Hours  float64 `json:"hours"`
		} `json:"entries"`
		TotalHours float64 `json:"totalHours"`
	}

	report.Data(t, &totals)

	if len(totals.Entries) != 0 || totals.TotalHours != 0 {
		t.Errorf("the administrator's report over somebody else's project is not empty: %+v",
			totals)
	}

	// Reading the list is still allowed: time has to be bookable against
	// something, and that list is what every employee sees anyway.
	var visible listOf[projectResponse]
	admin.must(admin.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &visible)

	found := false

	for _, project := range visible.Items {
		if project.ID == shared.ID {
			found = true
		}
	}

	if !found {
		t.Error("the administrator cannot see the shared project to book against")
	}
}

// The other half: what was taken away has to be exactly the work, or an
// installation is left unadministrable.
func TestTheAdministratorStillAdministersAndStillWorks(t *testing.T) {
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

	// Somebody else's working times: administering an account, as opposed to
	// reading what they recorded in it.
	admin.must(admin.api(http.MethodPut, path("/users/", created.ID, "/working-times"),
		map[string]any{"dailyTargetHours": 7}), http.StatusOK)

	// The installation itself.
	admin.must(admin.api(http.MethodGet, "/setup", nil), http.StatusOK)
	admin.must(admin.api(http.MethodPut, "/settings/operational",
		map[string]any{"maxDailyHours": 10}), http.StatusOK)

	// And their own time, like anybody who works here.
	var mine projectResponse
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Administration", "startDate": "2026-08-01", "private": true,
	}), http.StatusCreated, http.StatusOK).Data(t, &mine)

	var entry timesheetResponse
	admin.must(admin.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2, "projectId": mine.ID,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	admin.must(admin.api(http.MethodPut, path("/timesheets/", entry.ID),
		map[string]any{"status": "submitted"}), http.StatusOK)

	admin.must(admin.api(http.MethodGet, "/me/statistics?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK)

	var me struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)
	admin.must(admin.api(http.MethodGet, path("/users/", me.User.ID, "/overtime"), nil),
		http.StatusOK)
}

// What the guarantee is, exactly.
//
// It is the seeded default, not a wall: whoever may manage roles may widen the
// role they hold, and taking that away would take role administration with it. A
// user holds one role, so a wall would also mean a two-person team needed two
// accounts to approve an hour.
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
		"projects:write", "projects:archive", "projects:delete",
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
