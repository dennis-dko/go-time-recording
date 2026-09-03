package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// Both ways in arrive the same way.
//
// openTheStartingView takes the "loaded" mark back, chooses the first view, and
// gives the mark again - and its comment says what that mark is for: it is what
// anything outside this page has to go on, "a browser case, or a person wondering
// whether a blank card is empty or unanswered". refreshAll gives it as soon as
// every card is filled, which on the way in is one step early, because the first
// view has still to be chosen. A tab opened in that window "was switched away
// from a moment later, with nothing to show that anything had happened".
//
// The password sign-in was fixed to go through it. The passkey sign-in beside it
// called switchView(startingView()) directly, so the mark said ready while the
// interface was still about to change screens.
//
// The hashchange handler calls switchView(startingView()) too and is right to:
// that is navigation on a page which is already loaded and stays loaded, so there
// is no mark to take back. Arriving is the case this is about.
func TestBothSignInsOpenTheStartingViewTheSameWay(t *testing.T) {
	js := asset(t, "/app.js")

	for _, arrival := range []struct {
		what string
		body string
	}{
		{"the password sign-in", enclosing(t, js, "async function submitLogin(")},
		// The click handler, not the line in loadPasskeySupport that shows or hides
		// the same button - the first mention is not the one that signs anybody in.
		{"the passkey sign-in", enclosing(t, js, "$('#login-passkey').addEventListener")},
	} {
		if strings.Contains(arrival.body, "openTheStartingView(") {
			continue
		}

		t.Errorf("%s does not go through openTheStartingView, so the page calls "+
			"itself loaded while it is still about to switch screens. The other way "+
			"in does", arrival.what)
	}
}

// enclosing returns the source from a marker to the next top-level declaration,
// which is enough to hold the handler it starts.
func enclosing(t *testing.T, js, marker string) string {
	t.Helper()

	at := strings.Index(js, marker)
	if at < 0 {
		t.Fatalf("app.js no longer contains %q; this test is reading nothing", marker)
	}

	rest := js[at+len(marker):]

	end := regexp.MustCompile(`(?m)^(async function |function |// -----)`).FindStringIndex(rest)
	if end == nil {
		return js[at:]
	}

	return js[at : at+len(marker)+end[0]]
}
