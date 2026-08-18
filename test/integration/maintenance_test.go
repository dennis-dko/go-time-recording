//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

// Maintenance mode exists so nobody records time against a database that is
// about to be restored: work done in that window is lost when the snapshot goes
// back, and the person who did it has no way to know.
//
// That makes the interesting question not "does it block requests" but "can the
// administrator still end it". A mode that locks out the person who has to turn
// it off is a trap, and the only way to find out is to turn it on and try.

// setMaintenance turns the mode on or off.
func setMaintenance(t *testing.T, c *client, enabled bool, message string) {
	t.Helper()

	c.must(c.api(http.MethodPut, "/settings/maintenance", map[string]any{
		"enabled": enabled, "message": message,
	}), http.StatusOK)
}

// The point of the feature: ordinary work is refused, and refused in a way a
// monitor and a person both read correctly.
func TestMaintenanceModeTurnsOrdinaryWorkAway(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Worker", "email": "worker@example.com",
		"role": "user", "password": "worker-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("worker@example.com", "worker-password-1")

	// Working before.
	user.must(user.api(http.MethodGet, "/timesheets", nil), http.StatusOK)

	setMaintenance(t, admin, true, "Back at 14:00")

	response := user.api(http.MethodGet, "/timesheets", nil)
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", response.Status, response.Body)
	}

	// The message has to reach the person, or they call to ask.
	if !strings.Contains(string(response.Body), "Back at 14:00") {
		t.Errorf("the response does not carry the message: %s", response.Body)
	}

	// Booking time is the thing this is meant to prevent.
	if got := user.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-01", "durationHours": 2,
	}).Status; got != http.StatusServiceUnavailable {
		t.Errorf("booking during maintenance answered %d, want 503", got)
	}

	setMaintenance(t, admin, false, "")

	// The cache is short-lived, so this may need a moment.
	if !eventually(func() bool {
		return user.api(http.MethodGet, "/timesheets", nil).Status == http.StatusOK
	}) {
		t.Error("work is still refused after maintenance mode was turned off")
	}
}

// The trap this feature invites: turning it on and finding that the switch is
// behind it.
func TestTheAdministratorCanStillEndMaintenanceMode(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setMaintenance(t, admin, true, "maintenance")

	// Everything the administration screen needs has to keep answering.
	for _, path := range []string{
		"/me",
		"/maintenance",
		"/settings/datasource",
		"/settings/timezone",
		"/settings/operational",
		"/settings/telemetry",
		// The settings that need a restart are exactly the ones somebody is
		// likely to be changing while the installation is out of service.
		"/settings/restart",
		"/admin/logs",
	} {
		if got := admin.api(http.MethodGet, path, nil).Status; got != http.StatusOK {
			t.Errorf("%s answered %d during maintenance, want 200", path, got)
		}
	}

	// And the switch itself.
	setMaintenance(t, admin, false, "")

	var state MaintenanceOnTheWire

	admin.must(admin.api(http.MethodGet, "/maintenance", nil), http.StatusOK).Data(t, &state)

	if state.Enabled {
		t.Error("maintenance mode could not be turned off from inside it")
	}
}

// MaintenanceOnTheWire mirrors the response shape.
type MaintenanceOnTheWire struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

// A new session has to be obtainable during maintenance: an administrator whose
// cookie expired mid-window would otherwise be locked out of their own
// installation.
func TestSigningInStillWorksDuringMaintenance(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setMaintenance(t, admin, true, "maintenance")

	fresh := a.newClient()
	fresh.signIn(adminEmail, "a-much-better-password")

	fresh.must(fresh.api(http.MethodGet, "/me", nil), http.StatusOK)
}

// Whoever opens the page has to be told why, which means the notice is readable
// before there is a session.
func TestTheNoticeIsReadableWithoutSigningIn(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setMaintenance(t, admin, true, "Restoring a backup, back shortly")

	body := get(t, a.BaseURL()+"/api/v1/maintenance")
	if !strings.Contains(body, "Restoring a backup") {
		t.Errorf("the notice is not readable without a session: %s", truncate(body, 200))
	}

	// And the interface itself still loads, or the browser shows its own error
	// instead of the notice.
	if page := get(t, a.BaseURL()+"/"); !strings.Contains(page, "<title>") {
		t.Error("the interface is not served during maintenance, so nothing can show the notice")
	}
}

// Who gets past the notice: everybody who may administer the installation, and
// nobody else.
//
// The exemption used to be the built-in account and nothing else, which made
// maintenance mode a reason to sign in as it - the one account whose actions are
// hardest to attribute to a person, used for exactly the work where you most want
// to know who did it. It is now the same right the Settings screen is gated on,
// settings:manage, so "who may administer" has one answer.
//
// All three callers are checked in one instance, because what makes the answer
// worth anything is the contrast: the same installation, the same window, and one
// of them turned away.
func TestWhoeverMayAdministerGetsPastMaintenanceMode(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Somebody who works here and administers as well, and somebody who only
	// works here.
	other := a.signInAsWorkingAdmin(admin, "Second admin", "admin2@example.com")
	worker := a.signInAsUser(admin, "Wera", "wera@example.com")

	worker.must(worker.api(http.MethodGet, "/timesheets", nil), http.StatusOK)

	setMaintenance(t, admin, true, "maintenance")

	// /users rather than an exempt path: the administration screen keeps
	// answering for everybody, so a request that goes through it would prove
	// nothing about who is calling.
	if got := admin.api(http.MethodGet, "/users", nil).Status; got != http.StatusOK {
		t.Errorf("the built-in administrator got %d during maintenance, want 200", got)
	}

	if got := other.api(http.MethodGet, "/users", nil).Status; got != http.StatusOK {
		t.Errorf("an account holding the administration got %d during maintenance, want 200", got)
	}

	// And their own ordinary work, which is the half of that account maintenance
	// mode has no reason to stop once the person is let through at all.
	if got := other.api(http.MethodGet, "/timesheets", nil).Status; got != http.StatusOK {
		t.Errorf("an administrator's own timesheet answered %d during maintenance, want 200", got)
	}

	if got := worker.api(http.MethodGet, "/timesheets", nil).Status; got != http.StatusServiceUnavailable {
		t.Errorf("an ordinary account got %d during maintenance, want 503", got)
	}
}

// Nobody, which is not the same as everybody: the exemption is about the account
// behind the request, so a request that carries no account at all is refused.
func TestMaintenanceModeStillRefusesARequestWithoutASession(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setMaintenance(t, admin, true, "maintenance")

	anonymous := a.newClient()

	if got := anonymous.api(http.MethodGet, "/timesheets", nil).Status; got != http.StatusServiceUnavailable {
		t.Errorf("a request with no session got %d during maintenance, want 503", got)
	}
}

// An empty message still says something. "Temporarily unavailable" beats a bare
// status code.
func TestAnEmptyMessageFallsBackToSomethingReadable(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Reader", "email": "reader@example.com",
		"role": "user", "password": "reader-password-1",
	}), http.StatusCreated, http.StatusOK)

	user := a.newClient()
	user.signIn("reader@example.com", "reader-password-1")

	setMaintenance(t, admin, true, "")

	response := user.api(http.MethodGet, "/timesheets", nil)
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", response.Status)
	}

	if response.Message() == "" {
		t.Error("no message at all, so the person is left with a status code")
	}
}

// An over-long message is cut rather than refused: refusing would leave
// maintenance mode off while somebody edits their sentence, which is the opposite
// of what they were trying to do.
func TestAnOverLongMessageIsCutRatherThanRefused(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var stored MaintenanceOnTheWire

	admin.must(admin.api(http.MethodPut, "/settings/maintenance", map[string]any{
		"enabled": true, "message": strings.Repeat("x", 500),
	}), http.StatusOK).Data(t, &stored)

	if !stored.Enabled {
		t.Error("maintenance mode was not turned on")
	}

	if len(stored.Message) > 300 {
		t.Errorf("the stored message is %d characters, want it cut to 300", len(stored.Message))
	}

	setMaintenance(t, admin, false, "")
}
