//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Signing out takes the typed work with it, including what is not in a form.
//
// handBackTheScreen clears both halves of what somebody typed, and says why:
// forgetEveryDraft empties the store a reload would restore from, and the boxes
// on screen are the other one, because "signing out and letting somebody else
// sign in at the same desk left them looking at a card still holding the previous
// person's half-written database connection".
//
// It clears the boxes with `for (const form of $$('form')) form.reset()`. The
// stopwatch's project and description are deliberately not in a form - starting a
// timer is a button rather than a submission, which is the whole reason the
// "loose draft" machinery exists for them - so the reset walks straight past
// them. The store was emptied and the screen was not.
//
// What stays behind is free text: the description is what somebody is working on,
// which is exactly the thing not to hand to the next person at a shared desk.
//
// The same shape as the two finds before it in this rotation: a rule applied in
// one place and missing in the sibling that came later. Nothing sees it, because
// clearing the forms is correct and the loose controls are correctly not forms.
func TestSigningOutClearsTypedWorkThatIsNotInAForm(t *testing.T) {
	t.Parallel()

	p := open(t)
	// A worker, not the administrator: the stopwatch card is gated on
	// timesheets:write:own, which the built-in administrator does not hold - it
	// runs the installation rather than keeping a working day.
	p.readyWorker()

	const typed = "what the previous person was working on"

	p.run("open the stopwatch view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-description", chromedp.ByID))

	p.run("type into the loose control", chromedp.SendKeys(
		"#timer-description", typed, chromedp.ByID))

	var before string

	p.run("read it back", chromedp.Evaluate(
		`document.querySelector('#timer-description').value`, &before))

	if !strings.Contains(before, typed) {
		t.Fatalf("the field holds %q, so this case never typed anything and would "+
			"pass whatever the sign-out does", before)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#login-screen", chromedp.ByID))

	var after string

	p.run("read it after signing out", chromedp.Evaluate(
		`document.querySelector('#timer-description').value`, &after))

	if after != "" {
		t.Errorf("after signing out the stopwatch still holds %q. The next person "+
			"at this machine signs in and finds it: handBackTheScreen resets every "+
			"form, and this control is deliberately not in one", after)
	}
}
