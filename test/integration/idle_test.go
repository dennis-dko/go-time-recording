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

// What the page asks by itself does not keep an idle session.
//
// The page asks /me once a minute while it is in view, to notice rights that
// have changed, and asks after maintenance and new releases beside it; an open
// event stream reconnects when a proxy drops it. Every one of those is a request,
// and the timeout counted every request as somebody being there - so a screen
// left open at lunch, unlocked and in view, which is the screen this timeout is
// for, was never signed out at all. The page says which requests are its own, and
// those are still checked against the timeout without extending it.
func TestWhatThePageAsksByItselfDoesNotKeepAnIdleSession(t *testing.T) {
	t.Parallel()

	a := start(t, "SESSION_IDLE=2s")
	admin := a.signInAsAdmin("a-much-better-password")
	unattended := admin.withHeader("X-Background-Request", "1")

	ended := false

	// Every second, for well past the timeout, and nothing else.
	for range 6 {
		time.Sleep(time.Second)

		if unattended.api(http.MethodGet, "/me", nil).Status == http.StatusUnauthorized {
			ended = true

			break
		}
	}

	if !ended {
		t.Error("a session asked only by the page itself outlived a two-second idle timeout by four seconds")
	}
}

// Nor does the event stream reopening by itself.
//
// A browser opens the stream and opens it again whenever something between it
// and the server drops it, and a request opening it cannot carry a header to say
// nobody asked for it - so it is known by its path.
func TestTheEventStreamReopeningDoesNotKeepAnIdleSession(t *testing.T) {
	t.Parallel()

	a := start(t, "SESSION_IDLE=2s")
	admin := a.signInAsAdmin("a-much-better-password")
	unattended := admin.withHeader("X-Background-Request", "1")

	ended := false

	for range 6 {
		time.Sleep(time.Second)

		_, status, done := openFrames(t, admin)
		done()

		if status == http.StatusUnauthorized ||
			unattended.api(http.MethodGet, "/me", nil).Status == http.StatusUnauthorized {
			ended = true

			break
		}
	}

	if !ended {
		t.Error("a session whose event stream kept reopening outlived a two-second idle timeout by four seconds")
	}
}
