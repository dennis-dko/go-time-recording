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
// Enforced by the same per-endpoint checks that refuse a user, which is what
// these tests go through. A guarantee that only holds in the interface is not a
// guarantee; the interface hides what it cannot do, and a token client does not.

// userWithTime returns a user and one entry of theirs, booked by
// themselves - the administrator could not book it for them.
func userWithTime(t *testing.T, a *app, admin *client) (*client, timesheetResponse) {
	t.Helper()

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Nadja", "email": "nadja@example.com",
		"role": "user", "password": "nadja-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("nadja@example.com", "nadja-password-1")

	var entry timesheetResponse
	user.must(user.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 4, "description": "her own work",
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	return user, entry
}

func TestTheAdministratorCannotReachSomebodyElsesTime(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	owner, entry := userWithTime(t, a, admin)

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
	t.Parallel()

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
	// something, and that list is what every user sees anyway.
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
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Accounts and roles.
	var created struct {
		ID uint `json:"id"`
	}

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Olaf", "email": "olaf@example.com",
		"role": "user", "password": "olaf-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/roles", map[string]any{
		"name": "bookkeeping", "description": "reads its own figures",
		"permissions": []string{"reports:read:own", "timesheets:read:own"},
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

	// And it carries none either. It was seeded with the default eight, from when
	// this account recorded time - a figure nothing reads, since it cannot book an
	// hour or see a balance, and which showed up in the one place accounts are
	// listed as "8.0" beside every other row saying "default". Nobody could work
	// out what it meant, because it meant nothing.
	if self.User.DailyTargetHours != 0 {
		t.Errorf("the built-in administrator has a daily target of %v; it has no "+
			"working day to have one for, and the account table shows it",
			self.User.DailyTargetHours)
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
		"role": "user", "password": "hired-password-1",
	}), http.StatusCreated, http.StatusOK)
}

// What the guarantee is, exactly: a wall, and the way past it is a decision about a
// colleague rather than about itself.
//
// This case used to record the opposite. Whoever may manage roles could widen the role
// they hold, so the administrator could grant itself the right to read everybody's
// time, and that was written down as deliberate - the reasoning being that somebody who
// administers roles can reach anything anyway.
//
// That reasoning does not survive the arrangement this application has now. The
// built-in administrator configures the installation and keeps the accounts; it does
// not record time. A right added to its role would hand a working day to the one
// account nobody chose, quietly, from the screen that administers roles.
//
// So its permissions are fixed - neither given nor taken - and somebody who needs both
// jobs is given the combined role. That is a decision about a person, which is the
// point.
func TestTheAdministratorsRightsCannotBeChangedAtAll(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	_, entry := userWithTime(t, a, admin)

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
	// else's. What it must not have is a working day - the rights that record and
	// organise time, which belong to the people who do the work.
	//
	// The first four are gone from the application entirely, and are listed by name so
	// that reintroducing one and quietly seeding it here would be noticed. The rest
	// exist and belong to a user.
	for _, permission := range []string{
		"timesheets:read:all", "timesheets:write:all",
		"timesheets:approve", "reports:read",
		"timesheets:read:own", "timesheets:write:own",
		"timesheets:transfer",
		"projects:write", "settings:write:own",
	} {
		for _, has := range held {
			if has == permission {
				t.Errorf("the seeded admin role holds %q", permission)
			}
		}
	}

	// Widening is refused, which is the half that changed.
	//
	// With a right this application actually enforces, and one the administrator does
	// not hold: timesheets:read:own is a user's, and the administrator has no
	// working day. A name the application does not know would be refused too, for
	// being unknown, and would prove nothing about the wall.
	widened := admin.api(http.MethodPut, path("/roles/", adminRole), map[string]any{
		"permissions": append(append([]string{}, held...), "timesheets:read:own"),
	})

	if widened.Status == http.StatusOK {
		t.Error("the administrator granted itself a working day")
	}

	// Narrowing too, which it always was: the application looks this role up by name,
	// and stripping it would leave the installation unadministrable.
	narrowed := admin.api(http.MethodPut, path("/roles/", adminRole), map[string]any{
		"permissions": []string{"users:read"},
	})

	if narrowed.Status == http.StatusOK {
		t.Error("the administrator stripped its own role")
	}

	// Sending the set it already holds is not a change, and must not be refused as one:
	// a screen that returns its checkboxes in a different order would otherwise fail to
	// save for no reason anybody could see.
	admin.must(admin.api(http.MethodPut, path("/roles/", adminRole), map[string]any{
		"permissions": held,
	}), http.StatusOK)

	// And the description is refused too, which it was not for a long time. It
	// explains rather than grants, so it looked like the installation's business -
	// but the three shipped roles are the ones the interface translates, keyed on
	// the name, and a description edited here is overridden on screen by the
	// translation. Nothing about these roles is editable now, which is also what
	// their screen offers: they are shown rather than opened.
	described := admin.api(http.MethodPut, path("/roles/", adminRole), map[string]any{
		"description": "Runs this installation",
	})

	if described.Status != http.StatusConflict {
		t.Errorf("describing the administrator's role answered %d, want %d",
			described.Status, http.StatusConflict)
	}

	// A fresh session, because the session carries the permissions - so this cannot
	// pass by the old session simply not having noticed a grant.
	after := a.newClient()
	after.signIn(adminEmail, "a-much-better-password")

	if got := after.api(http.MethodGet, path("/timesheets/", entry.ID), nil).Status; got ==
		http.StatusOK {
		t.Error("the administrator can read somebody else's entry after all")
	}

	// The way past the wall: a person who works here and also administers. Note what it
	// does not buy - the combined role is a user's rights plus the administration,
	// so it holds its own working day and still nobody else's time.
	both := a.signInAsWorkingAdmin(admin, "Bothe", "bothe@example.com")

	both.must(both.api(http.MethodPost, "/users", map[string]any{
		"name": "Hired", "email": "hired@example.com",
		"role": "user", "password": "hired-password-1",
	}), http.StatusCreated, http.StatusOK)

	both.must(both.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 2,
	}), http.StatusCreated, http.StatusOK)

	if got := both.api(http.MethodGet, path("/timesheets/", entry.ID), nil).Status; got ==
		http.StatusOK {
		t.Error("the combined role reads somebody else's entry; it is a user's " +
			"rights plus the administration, and a user reads only their own")
	}
}

// A role has to grant something.
//
// One that grants nothing can be created and assigned, and whoever holds it signs in to
// an interface with almost nothing on it and no screen that matters - which reads as a
// broken installation rather than as a decision. Taking somebody's access away is what
// removing the account is for.
func TestARoleMustGrantAtLeastOnePermission(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, attempt := range []struct {
		what string
		body map[string]any
	}{
		{"no permissions at all", map[string]any{
			"name": "hollow", "description": "grants nothing"}},
		{"an empty list", map[string]any{
			"name": "hollow", "description": "grants nothing", "permissions": []string{}}},
		{"a list of blanks", map[string]any{
			"name": "hollow", "description": "grants nothing",
			"permissions": []string{"", "  "}}},
	} {
		r := admin.api(http.MethodPost, "/roles", attempt.body)

		if r.Status == http.StatusCreated || r.Status == http.StatusOK {
			t.Errorf("a role with %s was created", attempt.what)

			continue
		}

		if r.Message() == "" {
			t.Errorf("a role with %s was refused without saying why", attempt.what)
		}
	}

	// And one that grants something is created, so this is not simply refusing
	// everything.
	admin.must(admin.api(http.MethodPost, "/roles", map[string]any{
		"name": "reader", "description": "reads the accounts",
		"permissions": []string{"users:read"},
	}), http.StatusCreated, http.StatusOK)
}

// The directory synchronisation belongs to the built-in administrator, and so
// does scheduling it.
//
// Running it deletes every account the directory no longer holds, together with
// everything those people recorded, so the two buttons were always the built-in
// administrator's. The schedule beside them was not: it is the same deletion
// performed later and unattended, and it sat on the settings the combined role may
// write - so the safety the buttons were given could be walked around by typing
// five numbers into the field between them.
func TestOnlyTheBuiltInAdministratorSchedulesTheDirectoryRun(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	both := a.signInAsWorkingAdmin(admin, "Bothe", "bothe@example.com")

	// The combined role reaches the directory settings, which is what makes this
	// worth a test rather than being covered by the tab being absent.
	settings := both.must(both.api(http.MethodGet, "/settings/ldap", nil), http.StatusOK)

	var stored map[string]any
	settings.Data(t, &stored)

	// The connection is theirs to write.
	stored["host"] = "directory.example.com"
	both.must(both.api(http.MethodPut, "/settings/ldap", stored), http.StatusOK)

	// The schedule is not.
	stored["syncSchedule"] = "0 4 * * *"

	refused := both.api(http.MethodPut, "/settings/ldap", stored)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("scheduling the directory run answered %d, want %d",
			refused.Status, http.StatusForbidden)
	}

	// Nor by hand, which is the rule this one exists to close the way round.
	for _, route := range []string{"/settings/ldap/sync/preview", "/settings/ldap/sync"} {
		if got := both.api(http.MethodPost, route, nil).Status; got != http.StatusForbidden {
			t.Errorf("%s answered %d, want %d", route, got, http.StatusForbidden)
		}
	}

	// And the built-in administrator can do what the other was refused, so this is
	// not simply a broken endpoint.
	admin.must(admin.api(http.MethodPut, "/settings/ldap", stored), http.StatusOK)

	// Saving the connection again without touching the schedule still works: the
	// schedule travels with the rest of the directory settings, so refusing every
	// payload that carries one would refuse the combined role the connection form.
	both.must(both.api(http.MethodGet, "/settings/ldap", nil), http.StatusOK).Data(t, &stored)
	stored["host"] = "directory2.example.com"
	both.must(both.api(http.MethodPut, "/settings/ldap", stored), http.StatusOK)
}

// An account holding the admin role is the built-in administrator in every way
// that matters.
//
// A handful of things asked whether the caller *was* the built-in account rather
// than whether it administers: the directory synchronisation, its schedule, and
// the setup wizard. So an installation that had handed its administration to a
// person still had to sign in as the account nobody can attribute to one - which
// is the opposite of what giving somebody the role was for.
//
// The test names both halves. What the granted administrator gains, and what the
// combined role deliberately does not: somebody who also books time keeps their
// own screens and was never meant to inherit a purge that deletes accounts.
func TestTheAdminRoleIsTreatedAsTheBuiltInAdministrator(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	granted := a.signInAsGrantedAdministrator(admin, "Gerd", "gerd@example.com")
	both := a.signInAsWorkingAdmin(admin, "Bothe", "bothe@example.com")

	// The setup wizard.
	granted.must(granted.api(http.MethodGet, "/setup", nil), http.StatusOK)

	if got := both.api(http.MethodGet, "/setup", nil).Status; got != http.StatusForbidden {
		t.Errorf("the combined role reached the setup wizard: %d, want %d",
			got, http.StatusForbidden)
	}

	// The directory run, both by hand and on a schedule. A preview against no
	// configured directory fails for its own reasons; what is asserted is that it
	// is not refused as forbidden, which is the question here.
	for _, route := range []string{"/settings/ldap/sync/preview", "/settings/ldap/sync"} {
		if got := granted.api(http.MethodPost, route, nil).Status; got == http.StatusForbidden {
			t.Errorf("%s refused an account holding the admin role", route)
		}

		if got := both.api(http.MethodPost, route, nil).Status; got != http.StatusForbidden {
			t.Errorf("%s answered the combined role %d, want %d",
				route, got, http.StatusForbidden)
		}
	}

	var stored map[string]any

	granted.must(granted.api(http.MethodGet, "/settings/ldap", nil), http.StatusOK).Data(t, &stored)
	stored["syncSchedule"] = "0 4 * * *"

	granted.must(granted.api(http.MethodPut, "/settings/ldap", stored), http.StatusOK)

	// A different one for the combined role, because only a *change* is refused -
	// sending back the schedule that is already stored is what somebody editing
	// the connection does, and refusing that would refuse them the whole form.
	stored["syncSchedule"] = "0 5 * * *"

	if got := both.api(http.MethodPut, "/settings/ldap", stored).Status; got != http.StatusForbidden {
		t.Errorf("the combined role scheduled the directory run: %d, want %d",
			got, http.StatusForbidden)
	}

	// And it has no working day either, which is the other half of being that
	// kind of administrator rather than merely reaching the same screens.
	for _, attempt := range []struct {
		what   string
		method string
		route  string
		body   map[string]any
	}{
		{"book time", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-03", "durationHours": 2}},
		{"keep a project", http.MethodPost, "/projects",
			map[string]any{"name": "Administration", "startDate": "2026-08-01"}},
	} {
		if got := granted.api(attempt.method, attempt.route, attempt.body).Status; got == http.StatusOK ||
			got == http.StatusCreated {
			t.Errorf("an account with the admin role could %s", attempt.what)
		}
	}
}
