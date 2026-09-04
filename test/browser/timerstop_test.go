//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Signing out stops the clock that was running.
//
// handBackTheScreen opens by stopping the timers, and says why: "both pollers ask
// with the session that is about to end, and a timer left running would keep
// asking with none and paint the screen with authentication failures." It stops
// four of them - the log poll, the permission poll, the announcement stream and
// the release watch.
//
// The stopwatch's is a fifth and was not among them. It is cleared in one place
// only, inside renderTimer, and only on a pass where nothing is running - so a
// sign-out with a clock going left setInterval firing once a second, painting one
// person's elapsed time onto a screen that had been handed back.
//
// What clears it afterwards is the next account's loadTimer, and that returns at
// its first line for an account without timesheets:write:own - which the role
// editor allows, since it offers every permission separately. For such an account
// nothing ever clears it, and the previous person's stopwatch goes on counting in
// the corner of their screen.
//
// The invariant is simpler than that route, so this checks the invariant: after
// signing out there is no clock left running.
func TestSigningOutStopsTheStopwatch(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("start the clock", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID),
		p.click("#timer-start"),
		chromedp.WaitVisible("#timer-stop", chromedp.ByID))

	// Long enough that the painter has certainly run at least once.
	time.Sleep(1500 * time.Millisecond)

	var ticking string

	p.run("the clock is going", chromedp.Evaluate(
		`document.querySelector('#timer-elapsed').textContent.trim()`, &ticking))

	if ticking == "" {
		t.Fatal("the clock was not painting, so this case would pass whatever the " +
			"sign-out does")
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var afterSignOut string

	p.run("read it as the screen is handed back", chromedp.Evaluate(
		`document.querySelector('#timer-elapsed').textContent.trim()`, &afterSignOut))

	// Two seconds is two paints. A cleared interval leaves the text where it was;
	// a live one moves it on.
	time.Sleep(2 * time.Second)

	var later string

	p.run("read it again", chromedp.Evaluate(
		`document.querySelector('#timer-elapsed').textContent.trim()`, &later))

	if later != afterSignOut {
		t.Errorf("the stopwatch went on counting after the sign-out: %q became %q. "+
			"handBackTheScreen stops four timers and states the rule; this is the "+
			"fifth, and it is cleared only by a renderTimer pass that the next "+
			"account's loadTimer will not make if it may not write time entries",
			afterSignOut, later)
	}

	if later != "" {
		t.Errorf("the screen was handed back still showing %q on the stopwatch, "+
			"which is the previous account's elapsed time", later)
	}
}
