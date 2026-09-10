//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The end-to-end path a person actually walks: sign in, change the password,
// book time, see it in the table.
func TestBookingTimeThroughTheInterface(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// The server refuses everything until the initial password is replaced, so
	// this is not optional decoration - it is the only way in.
	p.settleWizard()

	p.run("open My account",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-password", chromedp.ByID))

	// Before a key is typed, not after. The form is in the markup, so it is on
	// screen at once, and the load that follows signing in ends by filling the
	// forms on this screen - a fill that lands between the typing and the press
	// puts the boxes back the way it found them and submits nothing. The server
	// refuses that, the banner stays, and the case waits twenty seconds for it to
	// go and reports the banner rather than the empty save that kept it there.
	p.settled()

	p.run("change the password",
		chromedp.SendKeys(`#form-password input[name="currentPassword"]`,
			harness.AdminPassword, chromedp.ByQuery),
		chromedp.SendKeys(`#form-password input[name="newPassword"]`,
			"a-much-better-password", chromedp.ByQuery),
		p.click(`#form-password button[type="submit"]`),
	)

	// The session survives the change now: the server ends the other devices and
	// keeps this one, because this is the device that just proved it knew the old
	// password. Waiting for a sign-in screen here would wait for ever.
	//
	// Waited on the effect rather than on six hundred milliseconds. The server
	// refuses everything else until this lands, so acting too early is refused
	// with "the initial password must be changed" - which arrives at whatever the
	// caller was doing three steps later as a 409 nobody can place. That is what
	// it did on a loaded runner: a case that creates an account got the refusal
	// and reported it as an account that already existed.
	//
	// The banner is driven by the same fact the server is checking, so its going
	// is the change having taken.
	p.waitGone("#password-banner")
	p.settled()

	if p.visible("#login-screen") {
		p.t.Fatalf("changing the password signed the administrator out; the session was meant to survive it")
	}

	p.settleWizard()

	// And now as somebody who works here. The administrator got this far because the
	// initial password has to be replaced before anything answers and only it can open
	// an account - but it records no time, so the booking below is not its to make.
	p.becomeWorker()

	p.run("book time",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="date"]`, "2026-08-03", chromedp.ByQuery),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "6.5", chromedp.ByQuery),
		chromedp.SendKeys(`#form-timesheet input[name="description"]`,
			"Booked in a browser", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	// The table has to show it. A booking the server accepted and the table
	// never renders is indistinguishable, to the person, from one that failed.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "Booked in a browser") {
			return
		}

		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("the booking never appeared in the table\n\ntable:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}
