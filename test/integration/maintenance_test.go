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
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Worker", "email": "worker@example.com",
		"role": "employee", "password": "worker-password-1",
	}), http.StatusCreated, http.StatusOK)

	employee := a.newClient()
	employee.signIn("worker@example.com", "worker-password-1")

	// Working before.
	employee.must(employee.api(http.MethodGet, "/timesheets", nil), http.StatusOK)

	setMaintenance(t, admin, true, "Back at 14:00")

	response := employee.api(http.MethodGet, "/timesheets", nil)
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", response.Status, response.Body)
	}

	// The message has to reach the person, or they call to ask.
	if !strings.Contains(string(response.Body), "Back at 14:00") {
		t.Errorf("the response does not carry the message: %s", response.Body)
	}

	// Booking time is the thing this is meant to prevent.
	if got := employee.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-01", "durationHours": 2,
	}).Status; got != http.StatusServiceUnavailable {
		t.Errorf("booking during maintenance answered %d, want 503", got)
	}

	setMaintenance(t, admin, false, "")

	// The cache is short-lived, so this may need a moment.
	if !eventually(func() bool {
		return employee.api(http.MethodGet, "/timesheets", nil).Status == http.StatusOK
	}) {
		t.Error("work is still refused after maintenance mode was turned off")
	}
}

// The trap this feature invites: turning it on and finding that the switch is
// behind it.
func TestTheAdministratorCanStillEndMaintenanceMode(t *testing.T) {
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

// An ordinary administrator is turned away like everyone else. Not because they
// are less trusted, but because "nothing is writing to this database" is the
// point, and every exception is a way for that to be untrue.
func TestAnOrdinaryAdministratorIsAlsoTurnedAway(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Second admin", "email": "admin2@example.com",
		"role": "admin", "password": "admin2-password-1",
	}), http.StatusCreated, http.StatusOK)

	other := a.newClient()
	other.signIn("admin2@example.com", "admin2-password-1")
	other.must(other.api(http.MethodGet, "/timesheets", nil), http.StatusOK)

	setMaintenance(t, admin, true, "maintenance")

	if got := other.api(http.MethodGet, "/timesheets", nil).Status; got != http.StatusServiceUnavailable {
		t.Errorf("a second administrator got %d for ordinary work, want 503", got)
	}
}

// An empty message still says something. "Temporarily unavailable" beats a bare
// status code.
func TestAnEmptyMessageFallsBackToSomethingReadable(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Reader", "email": "reader@example.com",
		"role": "employee", "password": "reader-password-1",
	}), http.StatusCreated, http.StatusOK)

	employee := a.newClient()
	employee.signIn("reader@example.com", "reader-password-1")

	setMaintenance(t, admin, true, "")

	response := employee.api(http.MethodGet, "/timesheets", nil)
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
