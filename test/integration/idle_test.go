//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
)

// A session that nobody uses ends, and one in use does not.
//
// The lifetime a session already had answers "how long is one sign-in worth",
// which is not the same question as "is anybody still there". An office that
// wants a screen left open at lunch to stop being a signed-in screen needs the
// second one, and there was no way to ask for it.
//
// Two seconds here because a case that waits out a real timeout is a case
// nobody runs. The environment is not held to the five-minute minimum the
// screen enforces - that minimum exists so an administrator cannot sign
// everybody out while they read, and this is not an administrator.
func TestASessionThatNobodyUsesEnds(t *testing.T) {
	t.Parallel()

	a := start(t, "SESSION_IDLE=2s")
	admin := a.signInAsAdmin("a-much-better-password")

	// In use: answered now, and answered again straight away.
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK)
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK)

	time.Sleep(3 * time.Second)

	// Left alone for longer than the timeout, and the cookie stops being worth
	// anything - which is the whole point of it.
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusUnauthorized)
}

// Working keeps a session, however long the working goes on.
//
// The obvious way to get this wrong is to measure from the sign-in, which is
// what the lifetime already does: an idle timeout that ends a session somebody
// is using is not an idle timeout, it is a shorter lifetime with a confusing
// name.
func TestASessionInConstantUseSurvivesTheIdleTimeout(t *testing.T) {
	t.Parallel()

	a := start(t, "SESSION_IDLE=3s")
	admin := a.signInAsAdmin("a-much-better-password")

	// Six seconds of work in two-second steps: twice the timeout, never idle for
	// it.
	for range 3 {
		time.Sleep(2 * time.Second)
		admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK)
	}
}

// An installation that has set no timeout never ends a session for idleness.
//
// Which is what every installation has until somebody decides otherwise:
// signing people out of a screen they left open is a decision about how an
// office works, and turning it on for everybody on the day they update is not
// that decision being made.
func TestWithNoIdleTimeoutASessionIsNeverEndedForIdleness(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	time.Sleep(3 * time.Second)

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK)
}
