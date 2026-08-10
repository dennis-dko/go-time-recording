//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// ---------------------------------------------------------------- projects

func TestAProjectIsInvisibleToEverybodyButItsOwner(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, name := range []string{"Erika", "Frank"} {
		admin.must(admin.api(http.MethodPost, "/users", map[string]any{
			"name": name, "email": lower(name) + "@example.com",
			"role": "employee", "password": lower(name) + "-password-1",
		}), http.StatusCreated, http.StatusOK)
	}

	// Somebody else's project. There used to be a shared kind here, visible to
	// everybody; every project belongs to one person now, so this one is Mila's and
	// nobody else's business.
	other := a.signInAsUser(admin, "Mila", "mila@example.com")
	other.must(other.api(http.MethodPost, "/projects", map[string]any{
		"name": "Mila's work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK)

	erika := a.newClient()
	erika.signIn("erika@example.com", "erika-password-1")

	var private projectResponse
	erika.must(erika.api(http.MethodPost, "/projects", map[string]any{
		"name": "Erika's own", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &private)

	if private.OwnerID == nil {
		t.Fatal("the project was created without an owner, so nobody can see it")
	}

	// Hers, and only hers: Mila's is not in her list either.
	var hers listOf[projectResponse]
	erika.must(erika.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &hers)

	if len(hers.Items) != 1 {
		t.Errorf("she should see her own project and no other, got %d", len(hers.Items))
	}

	frank := a.newClient()
	frank.signIn("frank@example.com", "frank-password-1")

	var his listOf[projectResponse]
	frank.must(frank.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &his)

	// Frank has created nothing, so he sees nothing - not Erika's, not Mila's.
	if len(his.Items) != 0 {
		t.Errorf("somebody who has created no project sees %d: %+v", len(his.Items), his.Items)
	}

	// Fetching it by id answers 404 rather than 403 - confirming the id exists
	// would be a way to enumerate what other people have.
	if r := frank.api(http.MethodGet, path("/projects/", private.ID), nil); r.Status != http.StatusNotFound {
		t.Errorf("expected 404 for someone else's private project, got %d", r.Status)
	}
}

// Hiding the project record is not the same as hiding the work booked against
// it, and for a while it was all that happened.
//
// Three endpoints reached past the visibility rule the rest of the application
// applies: the report totalled the hours on any project id it was given, the
// transfer moved an entry onto any project id, and archiving closed any project
// id. So somebody else's private category was readable, writable and closable by
// anyone holding the ordinary reporting, transfer or archiving permission - and
// each request also confirmed that the id existed.
func TestSomebodyElsesPrivateProjectIsHiddenFromReportsAndTransfers(t *testing.T) {
	a, admin, _ := startWithWorker(t)

	// Two ordinary accounts. Archiving somebody's project needs rights over the work,
	// which the built-in administrator does not hold any more - so it can no longer be
	// the caller this rule is proved against: it would be refused for the wrong
	// reason.
	gerda := a.signInAsUser(admin, "Gerda", "gerda@example.com")
	// An auditor: a role that may read everybody's time, which no default role is.
	// Building one is what an administrator can still do.
	heiko := a.signInAsAuditor(admin, "Heiko", "heiko@example.com")

	var hers projectResponse
	gerda.must(gerda.api(http.MethodPost, "/projects", map[string]any{
		"name": "Gerda's own", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &hers)

	gerda.must(gerda.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-04", "durationHours": 2, "projectId": hers.ID,
	}), http.StatusCreated, http.StatusOK)

	// The other account is the caller to prove this with: it holds every right over
	// including the report - which makes it the right one to prove the rule with,
	// because a rule that only holds against an employee is not a rule.
	if got := heiko.api(http.MethodGet,
		path("/projects/", hers.ID)+"/report", nil).Status; got != http.StatusNotFound {
		t.Errorf("a report on somebody else's private project answered %d, want 404", got)
	}

	// A shared project to move an entry onto, and an entry of the caller's own
	// to try moving - so the refusal can only be about the target's visibility.
	var shared projectResponse
	heiko.must(heiko.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &shared)

	var own timesheetResponse
	heiko.must(heiko.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-04", "durationHours": 1, "projectId": shared.ID,
	}), http.StatusCreated, http.StatusOK).Data(t, &own)

	if got := heiko.api(http.MethodPost, path("/timesheets/", own.ID)+"/transfer",
		map[string]any{"projectId": hers.ID}).Status; got != http.StatusNotFound {
		t.Errorf("transferring onto somebody else's private project answered %d, want 404", got)
	}

	// Archiving would take her own project away from her.
	if got := heiko.api(http.MethodPost,
		path("/projects/", hers.ID)+"/archive", nil).Status; got != http.StatusNotFound {
		t.Errorf("archiving somebody else's private project answered %d, want 404", got)
	}

	// And the owner is unaffected: the rule is about who is asking, not about a
	// private project becoming unreachable.
	//
	// Checked through the project rather than through its report: a report over her
	// private category would total her own hours, which proves nothing about whether
	// somebody else may see it. Her own project is the same request that answered 404
	// for the auditor a moment ago.
	gerda.must(gerda.api(http.MethodGet, path("/projects/", hers.ID), nil), http.StatusOK)

	var stillThere listOf[timesheetResponse]

	gerda.must(gerda.api(http.MethodGet, path("/timesheets?projectId=", hers.ID), nil),
		http.StatusOK).Data(t, &stillThere)

	if len(stillThere.Items) != 1 {
		t.Errorf("the owner sees %d of her own entries, want 1", len(stillThere.Items))
	}
}

// Recording time means recording the time that was worked. The form used to
// carry step="0.25", which did not round anything - it made the browser refuse
// the submit - while the API accepted any duration from a token client.
func TestAnyDurationCanBeBookedNotOnlyQuarterHours(t *testing.T) {
	_, _, worker := startWithWorker(t)

	for _, hours := range []float64{1.37, 0.1167, 7.99, 0.01} {
		var entry timesheetResponse

		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": "2026-08-06", "durationHours": hours,
		}), http.StatusCreated, http.StatusOK).Data(t, &entry)

		// Stored as given: a double column and plain addition all the way, so
		// what comes back is what went in rather than a rounded neighbour.
		if entry.DurationHours != hours {
			t.Errorf("booked %v and got back %v", hours, entry.DurationHours)
		}
	}

	// And a duration below the published floor is refused rather than stored as
	// something nobody meant.
	if got := worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-06", "durationHours": 0.001,
	}).Status; got == http.StatusCreated || got == http.StatusOK {
		t.Error("a duration below the documented minimum was accepted")
	}
}

func TestTimeCanBeBookedWithoutAProjectAndCategorisedLater(t *testing.T) {
	_, _, worker := startWithWorker(t)

	var entry timesheetResponse
	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-02", "durationHours": 3, "description": "Sort this out later",
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	if entry.ProjectID != nil {
		t.Fatal("a project must be optional on a booking")
	}

	// Private, because what is being tested is categorising an entry afterwards -
	// and a private project is one the administrator may create, where a shared one
	// belongs to whoever runs the work.
	var project projectResponse
	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Found a home", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	var updated timesheetResponse
	worker.must(worker.api(http.MethodPut, path("/timesheets/", entry.ID),
		map[string]any{"projectId": project.ID}), http.StatusOK).Data(t, &updated)

	if updated.ProjectID == nil || *updated.ProjectID != project.ID {
		t.Error("the entry should now belong to the project")
	}
}

// --------------------------------------------------------------- timezones

func TestTimezoneIsInstanceWideWithAPersonalOverride(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPut, "/settings/timezone",
		map[string]string{"timezone": "Europe/Berlin"}), http.StatusOK)

	var me struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.Timezone != "" {
		t.Errorf("no personal zone should be set, got %q", me.User.Timezone)
	}

	if me.User.EffectiveTimezone != "Europe/Berlin" {
		t.Errorf("the instance zone should apply, got %q", me.User.EffectiveTimezone)
	}

	// Someone working from abroad overrides it for themselves.
	admin.must(admin.api(http.MethodPut, "/me/timezone",
		map[string]string{"timezone": "Pacific/Auckland"}), http.StatusOK)

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.EffectiveTimezone != "Pacific/Auckland" {
		t.Errorf("the personal zone should win, got %q", me.User.EffectiveTimezone)
	}

	// Clearing it follows the instance again - the normal way back.
	admin.must(admin.api(http.MethodPut, "/me/timezone",
		map[string]string{"timezone": ""}), http.StatusOK)

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.EffectiveTimezone != "Europe/Berlin" {
		t.Errorf("clearing it should follow the instance, got %q", me.User.EffectiveTimezone)
	}
}

// A name that reads like a zone but is not one would be stored and then fall
// back to UTC at every use, moving bookings between days with nothing on
// screen to show it.
func TestAnUnknownTimezoneIsRefused(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, name := range []string{"Europe/Munich", "GMT+2", "Nowhere"} {
		if r := admin.api(http.MethodPut, "/settings/timezone",
			map[string]string{"timezone": name}); r.Status != http.StatusBadRequest {
			t.Errorf("%q should be refused, got %d", name, r.Status)
		}
	}

	// The tz database has to be compiled into the binary, or every zone would
	// resolve to UTC on a host without zoneinfo files - a scratch container.
	admin.must(admin.api(http.MethodPut, "/settings/timezone",
		map[string]string{"timezone": "Pacific/Chatham"}), http.StatusOK)
}

// ------------------------------------------------------- operational limits

func TestOperationalLimitsApplyWithoutARestart(t *testing.T) {
	// The two halves of this belong to two different jobs, so it takes two accounts.
	// The installation's ceiling is a setting, which the administrator owns; the
	// booking that proves the new ceiling is already in force is a working day, which
	// only somebody who works here has.
	_, admin, worker := startWithWorker(t)

	var before struct {
		Configured map[string]any `json:"configured"`
		Effective  struct {
			MaxDailyHours float64 `json:"maxDailyHours"`
		} `json:"effective"`
		Defaults struct {
			MaxDailyHours float64 `json:"maxDailyHours"`
		} `json:"defaults"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/operational", nil), http.StatusOK).Data(t, &before)

	if len(before.Configured) != 0 {
		t.Errorf("nothing should be overridden on a fresh instance, got %v", before.Configured)
	}

	if before.Effective.MaxDailyHours != before.Defaults.MaxDailyHours {
		t.Error("with nothing overridden, the effective value is the environment's")
	}

	admin.must(admin.api(http.MethodPut, "/settings/operational",
		map[string]any{"maxDailyHours": 9}), http.StatusOK)

	// In force immediately: 10 hours must now be refused. A new account carries no
	// personal ceiling of its own, so the number the booking meets is the one that was
	// just administered.
	today := time.Now().Format("2006-01-02")
	if r := worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": today, "durationHours": 10,
	}); r.Status != http.StatusConflict {
		t.Errorf("the new cap should apply at once, got %d", r.Status)
	}

	// Resetting returns to the environment's value.
	admin.must(admin.api(http.MethodPut, "/settings/operational", map[string]any{}), http.StatusOK)

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": today, "durationHours": 10,
	}), http.StatusCreated, http.StatusOK)
}

// This is the one screen that can lock out its own administrator, so the
// bounds are what make it safe to offer at all.
func TestOperationalLimitsRejectValuesThatWouldLockTheInstance(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	refused := []map[string]any{
		{"sessionLifetimeHours": 0.001}, // signs everyone out mid-click
		{"rateLimit": 1},                // refuses the administrator's own sign-in
		{"maxDailyHours": 48},           // more hours than a day has
		{"ldapSyncMaxDeleteRatio": 1.5}, // cannot mean anything
	}

	for _, body := range refused {
		if r := admin.api(http.MethodPut, "/settings/operational", body); r.Status != http.StatusBadRequest {
			t.Errorf("%v should be refused, got %d", body, r.Status)
		}
	}
}

// ------------------------------------------------------------- guided tour

func TestTourIsOfferedOnceAndCanBeRestarted(t *testing.T) {
	_, _, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.TourSeen {
		t.Fatal("a new account should be offered the tour")
	}

	worker.must(worker.api(http.MethodPut, "/me/tour", map[string]any{"seen": true}), http.StatusOK)
	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if !me.User.TourSeen {
		t.Error("the tour should be recorded as seen")
	}

	// Asking to see it again is allowed: being told you already had your chance
	// would be a strange way to treat someone who wants to look again.
	worker.must(worker.api(http.MethodPut, "/me/tour", map[string]any{"seen": false}), http.StatusOK)
	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.TourSeen {
		t.Error("the tour should be on offer again")
	}
}

// ------------------------------------------------------------------ health

func TestOperationalEndpointsAreServed(t *testing.T) {
	a := start(t)

	for _, path := range []string{"/.well-known/alive", "/.well-known/health"} {
		resp, err := http.Get(a.BaseURL() + path)
		if err != nil {
			t.Errorf("%s: %v", path, err)

			continue
		}

		_ = resp.Body.Close()

		// A container orchestrator restarts on these, so they have to answer
		// without a session.
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s should answer 200 without authentication, got %d", path, resp.StatusCode)
		}
	}
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}

	return string(out)
}

// Saving a record without editing any field must still work.
//
// This is where the dialects genuinely disagree: MySQL reports how many rows an
// UPDATE actually *changed*, PostgreSQL and SQLite how many it *matched*. Code
// that reads zero as "no such row" turns an ordinary save into a 404 - but only
// on MySQL, and only when nothing changed, which is why it went unnoticed until
// the suite was pointed at MySQL.
func TestSavingWithoutChangingAnythingIsNotAnError(t *testing.T) {
	// Every record saved here belongs to whoever it is about - their working times,
	// their project, their time entry - so the account doing the saving is somebody who
	// works here. The administrator holds no rights over any of the three, and a 403
	// would prove nothing about how many rows an UPDATE reported.
	_, _, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	// Twice with the same values. The second is the one that used to 404.
	for attempt := 1; attempt <= 2; attempt++ {
		r := worker.api(http.MethodPut, path("/users/", me.User.ID, "/working-times"),
			map[string]any{"dailyTargetHours": 8})

		if r.Status != http.StatusOK {
			t.Fatalf("attempt %d: saving unchanged working hours must succeed, got %d: %s",
				attempt, r.Status, r.Body)
		}
	}

	// The same for the other tables that are saved whole: a project and an entry of the
	// caller's own, which is the only kind of either there is.
	var project projectResponse
	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Unchanged", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	for attempt := 1; attempt <= 2; attempt++ {
		if r := worker.api(http.MethodPut, path("/projects/", project.ID),
			map[string]any{"name": "Unchanged", "startDate": "2026-08-01"}); r.Status != http.StatusOK {
			t.Errorf("project attempt %d: got %d: %s", attempt, r.Status, r.Body)
		}
	}

	var entry timesheetResponse
	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-01", "durationHours": 4,
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	for attempt := 1; attempt <= 2; attempt++ {
		if r := worker.api(http.MethodPut, path("/timesheets/", entry.ID),
			map[string]any{"durationHours": 4}); r.Status != http.StatusOK {
			t.Errorf("timesheet attempt %d: got %d: %s", attempt, r.Status, r.Body)
		}
	}
}
