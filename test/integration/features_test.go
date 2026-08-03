//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// ---------------------------------------------------------------- projects

func TestPrivateProjectsAreInvisibleToOthers(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, name := range []string{"Erika", "Frank"} {
		admin.must(admin.api(http.MethodPost, "/users", map[string]any{
			"name": name, "email": lower(name) + "@example.com",
			"role": "employee", "password": lower(name) + "-password-1",
		}), http.StatusCreated, http.StatusOK)
	}

	// A shared project, set up centrally.
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Shared work", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK)

	erika := a.newClient()
	erika.signIn("erika@example.com", "erika-password-1")

	var private projectResponse
	erika.must(erika.api(http.MethodPost, "/projects", map[string]any{
		"name": "Erika's own", "startDate": "2026-08-01", "private": true,
	}), http.StatusCreated, http.StatusOK).Data(t, &private)

	if !private.Private {
		t.Fatal("the project should have been created as private")
	}

	// Erika sees both; Frank sees only the shared one.
	var hers listOf[projectResponse]
	erika.must(erika.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &hers)

	if len(hers.Items) != 2 {
		t.Errorf("the owner should see both projects, got %d", len(hers.Items))
	}

	frank := a.newClient()
	frank.signIn("frank@example.com", "frank-password-1")

	var his listOf[projectResponse]
	frank.must(frank.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &his)

	for _, project := range his.Items {
		if project.Private {
			t.Errorf("someone else's private project is visible: %q", project.Name)
		}
	}

	// Fetching it by id answers 404 rather than 403 - confirming the id exists
	// would be a way to enumerate what other people have.
	if r := frank.api(http.MethodGet, path("/projects/", private.ID), nil); r.Status != http.StatusNotFound {
		t.Errorf("expected 404 for someone else's private project, got %d", r.Status)
	}
}

func TestTimeCanBeBookedWithoutAProjectAndCategorisedLater(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var entry timesheetResponse
	admin.must(admin.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-02", "durationHours": 3, "description": "Sort this out later",
	}), http.StatusCreated, http.StatusOK).Data(t, &entry)

	if entry.ProjectID != nil {
		t.Fatal("a project must be optional on a booking")
	}

	var project projectResponse
	admin.must(admin.api(http.MethodPost, "/projects", map[string]any{
		"name": "Found a home", "startDate": "2026-08-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &project)

	var updated timesheetResponse
	admin.must(admin.api(http.MethodPut, path("/timesheets/", entry.ID),
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
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

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

	// In force immediately: 10 hours must now be refused.
	today := time.Now().Format("2006-01-02")
	if r := admin.api(http.MethodPost, "/timesheets", map[string]any{
		"date": today, "durationHours": 10,
	}); r.Status != http.StatusConflict {
		t.Errorf("the new cap should apply at once, got %d", r.Status)
	}

	// Resetting returns to the environment's value.
	admin.must(admin.api(http.MethodPut, "/settings/operational", map[string]any{}), http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/timesheets", map[string]any{
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
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var me struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.TourSeen {
		t.Fatal("a new account should be offered the tour")
	}

	admin.must(admin.api(http.MethodPut, "/me/tour", map[string]any{"seen": true}), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if !me.User.TourSeen {
		t.Error("the tour should be recorded as seen")
	}

	// Asking to see it again is allowed: being told you already had your chance
	// would be a strange way to treat someone who wants to look again.
	admin.must(admin.api(http.MethodPut, "/me/tour", map[string]any{"seen": false}), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.TourSeen {
		t.Error("the tour should be on offer again")
	}
}

// ------------------------------------------------------------------ health

func TestOperationalEndpointsAreServed(t *testing.T) {
	a := start(t)

	for _, path := range []string{"/.well-known/alive", "/.well-known/health"} {
		resp, err := http.Get(a.baseURL + path)
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
