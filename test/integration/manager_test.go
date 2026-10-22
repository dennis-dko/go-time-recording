//go:build integration

package integration

import (
	"net/http"
	"slices"
	"testing"
)

// A manager is an ordinary account that also looks after the time of the accounts
// an administrator assigned to them - only while the installation has switched
// managers on, only those accounts, and in the open: the people looked after see
// who may read their time, and whatever a manager does to an entry carries the
// manager's name. These cases ask the endpoints, because the checks live in the
// handlers and what matters is what a request is answered with.

// team is an installation with managers: an administrator, a manager, the
// account assigned to them and one that is not.
type team struct {
	admin, manager, member, stranger *client
	managerID, memberID, strangerID  uint
}

// actedBy is the account named beside an entry as having booked or changed it.
type actedBy struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// managedEntry is an entry as a manager and its owner read it.
type managedEntry struct {
	timesheetResponse

	RecordedBy *actedBy `json:"recordedBy"`
	UpdatedBy  *actedBy `json:"updatedBy"`
}

// startTeam starts an instance and assigns Bea to Mona as her manager - with the
// installation's switch left as it ships, off, unless switchedOn says otherwise.
func startTeam(t *testing.T, switchedOn bool) *team {
	t.Helper()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	const password = "manager-password-1"

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Mona", "email": "mona@example.com", "role": "manager", "password": password,
	}), http.StatusCreated, http.StatusOK)

	manager := a.newClient()
	manager.signIn("mona@example.com", password)

	tm := &team{
		admin:    admin,
		manager:  manager,
		member:   a.signInAsUser(admin, "Bea", "bea@example.com"),
		stranger: a.signInAsUser(admin, "Sven", "sven@example.com"),
	}

	tm.managerID, tm.memberID, tm.strangerID = idOf(t, tm.manager), idOf(t, tm.member), idOf(t, tm.stranger)

	admin.must(admin.api(http.MethodPut, path("/users/", tm.memberID, "/managers"),
		map[string]any{"managerIds": []uint{tm.managerID}}), http.StatusOK)

	if switchedOn {
		admin.must(admin.api(http.MethodPut, "/settings/managers", map[string]any{"enabled": true}),
			http.StatusOK)
	}

	return tm
}

// idOf is the id of the account a client is signed in as.
func idOf(t *testing.T, c *client) uint {
	t.Helper()

	var me struct {
		User userResponse `json:"user"`
	}

	c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	return me.User.ID
}

// teamOf reads /me/team or /me/managers as the ids it names.
func teamOf(t *testing.T, c *client, route string) []uint {
	t.Helper()

	var listed listOf[struct {
		ID     uint     `json:"id"`
		Rights []string `json:"rights"`
	}]

	c.must(c.api(http.MethodGet, route, nil), http.StatusOK).Data(t, &listed)

	ids := []uint{}
	for _, item := range listed.Items {
		ids = append(ids, item.ID)
	}

	return ids
}

// An assignment reaches nothing until an administrator switches managers on, and
// both sides are told the moment it does: the manager who they look after, and the
// person looked after who may read their time.
func TestAManagerReachesNobodyUntilTheInstallationSwitchesManagersOn(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, false)
	entryOf(t, tm.member, map[string]any{"date": "2026-10-05", "durationHours": 3})

	refused := tm.manager.api(http.MethodGet, path("/timesheets?userId=", tm.memberID), nil)
	if refused.Status != http.StatusForbidden || errorCode(t, refused) != "onlyOwnEntriesRead" {
		t.Errorf("with managers switched off, reading Bea's entries answered %d %q, want 403 onlyOwnEntriesRead",
			refused.Status, errorCode(t, refused))
	}

	if got := teamOf(t, tm.manager, "/me/team"); len(got) != 0 {
		t.Errorf("with managers switched off, Mona's team is %v, want nobody", got)
	}

	if got := teamOf(t, tm.member, "/me/managers"); len(got) != 0 {
		t.Errorf("with managers switched off, Bea is told %v may see her time, want nobody", got)
	}

	tm.admin.must(tm.admin.api(http.MethodPut, "/settings/managers", map[string]any{"enabled": true}),
		http.StatusOK)

	var entries listOf[timesheetResponse]
	tm.manager.must(tm.manager.api(http.MethodGet, path("/timesheets?userId=", tm.memberID), nil),
		http.StatusOK).Data(t, &entries)

	if len(entries.Items) != 1 || entries.Items[0].UserID != tm.memberID {
		t.Errorf("with managers switched on, Mona reads %+v, want Bea's one entry", entries.Items)
	}

	if got := teamOf(t, tm.manager, "/me/team"); !slices.Equal(got, []uint{tm.memberID}) {
		t.Errorf("Mona's team is %v, want Bea (%d)", got, tm.memberID)
	}

	if got := teamOf(t, tm.member, "/me/managers"); !slices.Equal(got, []uint{tm.managerID}) {
		t.Errorf("Bea is told %v may see her time, want Mona (%d)", got, tm.managerID)
	}

	var me struct {
		ManagersEnabled bool `json:"managersEnabled"`
	}

	tm.member.must(tm.member.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if !me.ManagersEnabled {
		t.Error("/me does not say that the installation has managers")
	}
}

// A manager reaches the accounts assigned to them and nobody else: another
// account's entries are refused as a listing and are not there by id, exactly as
// before managers existed.
func TestAManagerReachesOnlyTheAccountsAssignedToThem(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, true)
	strangers := entryOf(t, tm.stranger, map[string]any{
		"date": "2026-10-05", "durationHours": 2, "description": "nobody's business",
	})

	listed := tm.manager.api(http.MethodGet, path("/timesheets?userId=", tm.strangerID), nil)
	if listed.Status != http.StatusForbidden {
		t.Errorf("listing an unassigned account's entries answered %d, want 403", listed.Status)
	}

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		answer := tm.manager.api(method, path("/timesheets/", strangers.ID),
			map[string]any{"durationHours": 1})
		if answer.Status != http.StatusNotFound {
			t.Errorf("%s on an unassigned account's entry answered %d, want 404", method, answer.Status)
		}
	}

	booked := tm.manager.api(http.MethodPost, "/timesheets", map[string]any{
		"userId": tm.strangerID, "date": "2026-10-06", "durationHours": 1,
	})
	if booked.Status != http.StatusForbidden {
		t.Errorf("booking for an unassigned account answered %d, want 403", booked.Status)
	}

	overtime := tm.manager.api(http.MethodGet, path("/users/", tm.strangerID, "/overtime"), nil)
	if overtime.Status != http.StatusForbidden || errorCode(t, overtime) != "onlyOwnOvertime" {
		t.Errorf("an unassigned account's balance answered %d %q, want 403 onlyOwnOvertime",
			overtime.Status, errorCode(t, overtime))
	}

	// And an ordinary account reaches nobody even when somebody names it: the team
	// rights are the manager role's, not the switch's.
	plain := tm.stranger.api(http.MethodGet, path("/timesheets?userId=", tm.memberID), nil)
	if plain.Status != http.StatusForbidden {
		t.Errorf("an ordinary account listing Bea's entries answered %d, want 403", plain.Status)
	}
}

// What a manager books or corrects carries their name, and the person it belongs to
// reads it - on the entry, and against each version the manager's correction
// replaced. Their own changes name nobody.
func TestWhatAManagerBooksOrChangesCarriesTheirName(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, true)

	var project projectResponse
	tm.member.must(tm.member.api(http.MethodPost, "/projects", map[string]any{
		"name": "Bea's work", "startDate": "2026-10-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	var booked managedEntry
	tm.manager.must(tm.manager.api(http.MethodPost, "/timesheets", map[string]any{
		"userId": tm.memberID, "projectId": project.ID, "date": "2026-10-05", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK).Data(t, &booked)

	if booked.UserID != tm.memberID || booked.RecordedBy == nil || booked.RecordedBy.ID != tm.managerID ||
		booked.RecordedBy.Name != "Mona" {
		t.Fatalf("Mona booked %+v (recorded by %+v), want Bea's entry recorded by Mona", booked, booked.RecordedBy)
	}

	var read managedEntry
	tm.member.must(tm.member.api(http.MethodGet, path("/timesheets/", booked.ID), nil), http.StatusOK).
		Data(t, &read)

	if read.RecordedBy == nil || read.RecordedBy.Name != "Mona" || read.UpdatedBy != nil {
		t.Errorf("Bea reads her entry as recorded by %+v and changed by %+v, want Mona and nobody",
			read.RecordedBy, read.UpdatedBy)
	}

	var corrected managedEntry
	tm.manager.must(tm.manager.api(http.MethodPut, path("/timesheets/", booked.ID),
		map[string]any{"durationHours": 5}), http.StatusOK).Data(t, &corrected)

	if corrected.UpdatedBy == nil || corrected.UpdatedBy.ID != tm.managerID {
		t.Errorf("Mona's correction is named as by %+v, want Mona", corrected.UpdatedBy)
	}

	var history listOf[struct {
		DurationHours float64  `json:"durationHours"`
		ReplacedBy    *actedBy `json:"replacedBy"`
	}]

	tm.member.must(tm.member.api(http.MethodGet, path("/timesheets/", booked.ID, "/revisions"), nil),
		http.StatusOK).Data(t, &history)

	if len(history.Items) != 1 || history.Items[0].DurationHours != 4 || history.Items[0].ReplacedBy == nil ||
		history.Items[0].ReplacedBy.Name != "Mona" {
		t.Errorf("Bea's history reads %+v, want the four hours, replaced by Mona", history.Items)
	}

	var own managedEntry
	tm.member.must(tm.member.api(http.MethodPut, path("/timesheets/", booked.ID),
		map[string]any{"durationHours": 6}), http.StatusOK).Data(t, &own)

	if own.UpdatedBy != nil || own.RecordedBy == nil {
		t.Errorf("after Bea's own correction the entry reads changed by %+v and recorded by %+v, "+
			"want nobody and still Mona", own.UpdatedBy, own.RecordedBy)
	}
}

// A manager sees that a day was away and never why: leave and sickness read the
// same to them, while the person they are about reads their own as they recorded
// them.
func TestAManagerSeesThatADayWasAwayButNeverWhy(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, true)

	tm.member.must(tm.member.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-10-06", "to": "2026-10-06", "kind": "sick",
	}), http.StatusCreated, http.StatusOK)
	tm.member.must(tm.member.api(http.MethodPost, "/me/absences", map[string]any{
		"from": "2026-10-07", "to": "2026-10-07", "kind": "halfVacation",
	}), http.StatusCreated, http.StatusOK)
	entryOf(t, tm.member, map[string]any{"date": "2026-10-07", "durationHours": 4})

	type day struct {
		Date    string `json:"date"`
		Absence string `json:"absence"`
	}

	absences := func(c *client, route string) map[string]string {
		var answer struct {
			Days []day `json:"days"`
		}

		c.must(c.api(http.MethodGet, route, nil), http.StatusOK).Data(t, &answer)

		found := map[string]string{}
		for _, d := range answer.Days {
			if d.Absence != "" {
				found[d.Date] = d.Absence
			}
		}

		return found
	}

	const week = "from=2026-10-05&to=2026-10-11"

	own := absences(tm.member, "/me/days?"+week)
	if own["2026-10-06"] != "sick" || own["2026-10-07"] != "halfVacation" {
		t.Errorf("Bea reads her own days as %v, want sick and halfVacation", own)
	}

	seen := absences(tm.manager, path("/me/days?", week, "&userId=", tm.memberID))
	if seen["2026-10-06"] != "away" || seen["2026-10-07"] != "away" {
		t.Errorf("Mona reads Bea's days as %v, want both away", seen)
	}

	balance := absences(tm.manager, path("/users/", tm.memberID, "/overtime?", week))
	if balance["2026-10-07"] != "away" {
		t.Errorf("Mona reads Bea's balance with the absences %v, want the booked day away", balance)
	}
}

// A manager keeps the projects of the accounts assigned to them - makes one for
// them, lists them, changes them - and nobody else's.
func TestAManagerKeepsTheProjectsOfTheAccountsAssignedToThem(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, true)

	var made projectResponse
	tm.manager.must(tm.manager.api(http.MethodPost, "/projects", map[string]any{
		"name": "Made for Bea", "startDate": "2026-10-01", "ownerId": tm.memberID,
	}), http.StatusCreated, http.StatusOK).Data(t, &made)

	if made.OwnerID == nil || *made.OwnerID != tm.memberID {
		t.Fatalf("the project Mona made for Bea belongs to %v, want Bea (%d)", made.OwnerID, tm.memberID)
	}

	var bea listOf[projectResponse]
	tm.member.must(tm.member.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &bea)

	if len(bea.Items) != 1 || bea.Items[0].ID != made.ID {
		t.Errorf("Bea lists %+v, want the project made for her", bea.Items)
	}

	var listed listOf[projectResponse]
	tm.manager.must(tm.manager.api(http.MethodGet, path("/projects?ownerId=", tm.memberID), nil),
		http.StatusOK).Data(t, &listed)

	if len(listed.Items) != 1 || listed.Items[0].ID != made.ID {
		t.Errorf("Mona lists Bea's projects as %+v, want the one she made", listed.Items)
	}

	tm.manager.must(tm.manager.api(http.MethodPut, path("/projects/", made.ID),
		map[string]any{"name": "Renamed for Bea"}), http.StatusOK)

	var strangers projectResponse
	tm.stranger.must(tm.stranger.api(http.MethodPost, "/projects", map[string]any{
		"name": "Sven's own", "startDate": "2026-10-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &strangers)

	if answer := tm.manager.api(http.MethodPut, path("/projects/", strangers.ID),
		map[string]any{"name": "Taken"}); answer.Status != http.StatusNotFound {
		t.Errorf("changing an unassigned account's project answered %d, want 404", answer.Status)
	}

	refused := tm.manager.api(http.MethodPost, "/projects", map[string]any{
		"name": "For Sven", "startDate": "2026-10-01", "ownerId": tm.strangerID,
	})
	if refused.Status != http.StatusForbidden || errorCode(t, refused) != "onlyOwnProjects" {
		t.Errorf("making a project for an unassigned account answered %d %q, want 403 onlyOwnProjects",
			refused.Status, errorCode(t, refused))
	}
}

// Only an administrator assigns, and only somebody who can look after an account
// can be assigned to it - never the account itself.
func TestOnlyAnAdministratorAssignsAndOnlyManagersCanBeAssigned(t *testing.T) {
	t.Parallel()

	tm := startTeam(t, true)

	own := tm.manager.api(http.MethodPut, path("/users/", tm.strangerID, "/managers"),
		map[string]any{"managerIds": []uint{tm.managerID}})
	if own.Status != http.StatusForbidden {
		t.Errorf("a manager assigning herself an account answered %d, want 403", own.Status)
	}

	for _, tc := range []struct {
		name     string
		userID   uint
		managers []uint
		code     string
	}{
		{"an ordinary account as a manager", tm.memberID, []uint{tm.strangerID}, "notAManager"},
		{"a manager of herself", tm.managerID, []uint{tm.managerID}, "selfManaged"},
	} {
		answer := tm.admin.api(http.MethodPut, path("/users/", tc.userID, "/managers"),
			map[string]any{"managerIds": tc.managers})
		if answer.Status != http.StatusBadRequest || errorCode(t, answer) != tc.code {
			t.Errorf("%s answered %d %q, want 400 %s", tc.name, answer.Status, errorCode(t, answer), tc.code)
		}
	}

	var assigned struct {
		ManagerIDs []uint `json:"managerIds"`
	}

	tm.admin.must(tm.admin.api(http.MethodGet, path("/users/", tm.memberID, "/managers"), nil),
		http.StatusOK).Data(t, &assigned)

	if !slices.Equal(assigned.ManagerIDs, []uint{tm.managerID}) {
		t.Errorf("Bea's managers read %v after the refusals, want Mona alone", assigned.ManagerIDs)
	}
}

// An account that goes takes its assignments with it, from both sides - which on
// PostgreSQL is the difference between deleting the account and the foreign key
// refusing it, and everywhere else between that and an assignment to nobody.
func TestDeletingAManagerOrTheirMemberTakesTheAssignmentWithThem(t *testing.T) {
	t.Parallel()

	for _, gone := range []string{"manager", "member"} {
		t.Run(gone, func(t *testing.T) {
			t.Parallel()

			tm := startTeam(t, true)

			deleted, kept, keptID := tm.managerID, tm.member, tm.memberID
			if gone == "member" {
				deleted, kept, keptID = tm.memberID, tm.manager, tm.managerID
			}

			tm.admin.must(tm.admin.api(http.MethodDelete, path("/users/", deleted), nil),
				http.StatusOK, http.StatusNoContent)

			route := "/me/managers"
			if gone == "member" {
				route = "/me/team"
			}

			if got := teamOf(t, kept, route); len(got) != 0 {
				t.Errorf("after deleting the %s, %s still reads %v", gone, route, got)
			}

			var assigned struct {
				ManagerIDs []uint `json:"managerIds"`
			}

			tm.admin.must(tm.admin.api(http.MethodGet, path("/users/", keptID, "/managers"), nil),
				http.StatusOK).Data(t, &assigned)

			if len(assigned.ManagerIDs) != 0 {
				t.Errorf("after deleting the %s, the account left has the managers %v", gone, assigned.ManagerIDs)
			}
		})
	}
}
