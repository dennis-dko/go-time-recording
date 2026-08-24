//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

// Signing in during maintenance, for somebody who may not be here while it
// lasts.
//
// The middleware cannot decide this. It runs in front of the router and sees a
// request with no session yet, so /auth/login is exempt as a whole - and the
// result was an ordinary account that signed in perfectly, was welcomed by the
// interface, and then met a 503 on every card it tried to fill. Signed in and
// unable to do anything, with nothing saying that was on purpose.
//
// The decision needs a name to make it about, so it is made in the handler,
// after the credentials are checked.
func TestMaintenanceTurnsAnOrdinaryAccountAwayAtTheDoor(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Worker", "email": "worker@example.com",
		"role": "user", "password": "worker-password-1",
	}), http.StatusCreated, http.StatusOK)

	// Before: an ordinary sign-in works, which is what makes the refusal below
	// about maintenance rather than about the password.
	before := a.newClient()
	before.signIn("worker@example.com", "worker-password-1")

	setMaintenance(t, admin, true, "Back at 14:00")

	user := a.newClient()

	response := user.api(http.MethodPost, "/auth/login",
		map[string]string{"email": "worker@example.com", "password": "worker-password-1"})

	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("signing in during maintenance got %d, want 503: %s",
			response.Status, response.Body)
	}

	// The administrator's own words, so nobody has to guess whether the
	// installation is broken or busy.
	if !strings.Contains(string(response.Body), "Back at 14:00") {
		t.Errorf("the refusal does not carry the notice: %s", response.Body)
	}

	// And no session came out of it. A refused sign-in that still hands out a
	// cookie is a sign-in that succeeded quietly: its holder would be signed in
	// the moment maintenance ended, without having proved anything since.
	if got := user.api(http.MethodGet, "/me", nil); got.Status == http.StatusOK {
		t.Errorf("the refused sign-in left a working session behind: %s", got.Body)
	}
}

// And the person who has to end it still gets in.
//
// The other half of the same rule, and the half that matters more: a mode that
// locks out whoever must turn it off is a trap. Asserted here beside the
// refusal, because the two are one decision and a change to either belongs
// against both.
func TestMaintenanceStillLetsAnAdministratorSignIn(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	setMaintenance(t, admin, true, "Back at 14:00")

	// A second sign-in, from a client with no cookies of its own: the question
	// is whether the door opens, not whether a session already through it still
	// works.
	again := a.newClient()

	response := again.api(http.MethodPost, "/auth/login",
		map[string]string{"email": adminEmail, "password": "a-much-better-password"})

	if response.Status != http.StatusOK && response.Status != http.StatusCreated {
		t.Fatalf("the administrator was locked out of their own maintenance mode: "+
			"got %d: %s", response.Status, response.Body)
	}

	// And the session works, which is what "signed in" has to mean here - the
	// administration screen is the only way back out.
	again.must(again.api(http.MethodGet, "/settings/datasource", nil), http.StatusOK)
}
