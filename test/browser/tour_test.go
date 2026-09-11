//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A first sign-in is walked through the application, without being asked first.
//
// Somebody arriving in an application nobody has introduced is the moment they
// decide it is complicated. The walk used to be offered by a modal with "Show me
// around" and "Not now" on it, and "Not now" recorded the tour as seen - so the
// button that looked like "later" meant "never", and the introduction this
// application has was the introduction almost nobody got.
func TestAFirstSignInIsWalkedThroughTheApplication(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// An ordinary user, because the built-in administrator is deliberately not
	// walked through: it arrives at the setup wizard, and a walk through booking
	// time would be a walk through somebody else's job.
	p.run("create a user",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Rieke", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "rieke@example.com", chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "rieke-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	// Slept through rather than waited for, and this is the one shape where that
	// is right: what follows asserts that something did *not* appear. There is no
	// condition to wait for - the answer is only trustworthy after giving it a
	// chance to be wrong.
	time.Sleep(500 * time.Millisecond)

	// The administrator was not, which is half the requirement.
	if p.visible("#tour-bubble") {
		t.Error("the built-in administrator was walked through the application")
	}

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("rieke@example.com", "rieke-password-1")
	p.waitGone("#login-screen")

	// No click anywhere: it starts by itself.
	p.run("wait for the walk through",
		chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	// The walk has to be a walk: the first step counts itself, and Next moves on.
	first := p.text("#tour-title")

	if first == "" {
		t.Error("the first step has no title")
	}

	if count := p.text("#tour-count"); count == "" {
		t.Error("the tour does not say where in it you are")
	}

	// Long enough to be the tour of an application rather than of one screen. Every
	// step outside the screen it started on used to be dropped - the reachability
	// check asked for offsetParent, which is null for everything inside a hidden
	// view - so a walk begun on the time entries was four steps long and looked
	// complete.
	if total := tourTotal(p); total < 12 {
		t.Errorf("the walk is %d steps, which is one screen's worth rather than the "+
			"whole application", total)
	}

	p.run("next step", p.click("#tour-next"))
	if second := p.waitChanged("#tour-title", first); second == first {
		t.Errorf("Next did not move on; still on %q", first)
	}

	p.run("leave the tour", p.click("#tour-end"))
	// An absence again: see above.
	time.Sleep(400 * time.Millisecond)

	if p.visible("#tour-bubble") {
		t.Error("skipping did not end the tour")
	}

	// Seen once: a reload must not start it again, or leaving it would mean
	// nothing.
	p.run("reload", chromedp.Reload())
	p.waitGone("#login-screen")

	// And once more: nothing to wait for, only time to give it.
	time.Sleep(800 * time.Millisecond)

	if p.visible("#tour-bubble") {
		t.Error("the walk through started again after being left")
	}
}

// tourTotal reads how many steps the walk has, out of "Step 1 of 20".
func tourTotal(p *page) int {
	fields := strings.Fields(p.text("#tour-count"))
	if len(fields) == 0 {
		return 0
	}

	total, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return 0
	}

	return total
}
