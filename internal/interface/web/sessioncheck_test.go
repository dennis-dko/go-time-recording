package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The sign-in form is not on screen while the session is still being checked.
//
// A reload used to show the login screen for a moment and then jump to whichever
// screen was open, which reads as being signed out and signed straight back in
// again. The cause is structural rather than accidental: the page is markup, the
// session is a request, and the markup is on screen first. Whatever it says
// during that request is what a reload looks like.
//
// The arrangement that fixes it is three things that have to agree - the screen
// ships carrying "checking", the stylesheet hides its contents in that state and
// shows the spinner instead, and the script takes the class off once the answer
// is in. Any one of them changing alone brings the flash back, and none of them
// looks wrong on its own. A browser cannot catch this reliably either: the window
// is one request wide, so a test that samples for it passes on a fast machine.
//
// So it is asserted where it is decided.
func TestTheSignInFormIsHiddenWhileTheSessionIsChecked(t *testing.T) {
	markup := readAsset(t, "index.html")

	screen := regexp.MustCompile(`<div id="login-screen"[^>]*>`).FindString(markup)
	if screen == "" {
		t.Fatal("there is no sign-in screen in the markup")
	}

	// Shipped in the checking state. Served markup is the very first thing on
	// screen, so anything decided after the first paint is decided too late.
	if !strings.Contains(screen, "checking") {
		t.Errorf("the sign-in screen ships as %q, so it is on screen before "+
			"anything has asked whether there is a session", screen)
	}

	// And it holds something to show instead, or the reload would flash an empty
	// card rather than a form.
	if !strings.Contains(markup, "session-check") {
		t.Error("nothing stands in for the form while the session is checked")
	}

	style := readAsset(t, "app.css")

	for _, rule := range []string{
		".login-screen.checking > *:not(.session-check)",
		".login-screen.checking .session-check",
	} {
		if !strings.Contains(style, rule) {
			t.Errorf("the stylesheet has no rule for %s, so the checking state "+
				"changes nothing about what is drawn", rule)
		}
	}

	script := readAsset(t, "app.js")

	// Taken off in both directions. Removing it only where a session was found
	// leaves somebody who is genuinely signed out looking at a spinner instead of
	// the form they need.
	if got := strings.Count(script, `classList.remove('checking')`); got < 2 {
		t.Errorf("the checking state is lifted in %d place(s); it has to go both "+
			"when a session was found and when there is none", got)
	}
}

// readAsset reads one of the served files from the source tree.
func readAsset(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("assets", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	return string(body)
}
