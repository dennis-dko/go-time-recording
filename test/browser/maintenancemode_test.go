//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The switch has to work from the screen it lives on, and the notice has to be
// visible afterwards - including on the sign-in screen, which is the only place
// somebody turned away can read anything at all.
func TestTurningMaintenanceModeOnAndOffFromTheInterface(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	p.run("turn it on",
		chromedp.SendKeys(`#form-maintenance input[name="message"]`, "Restoring a backup", chromedp.ByQuery),
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
		// Switching it on asks first. The question is in the page rather than
		// drawn by the browser, so it can simply be answered - a native dialog
		// had to be intercepted, because a headless browser has nobody to click
		// it and an unanswered one blocks every later action.
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
	)

	p.run("confirm", p.click(`.confirm-actions button.danger`))

	p.waitForText("#maintenance-banner", "Restoring a backup")

	// Signed out, the notice is what a person sees instead of silence.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#maintenance-banner") {
		t.Error("the notice is not shown on the sign-in screen, where it matters most")
	}

	// And back in, the administrator can end it.
	p.signIn(harness.AdminEmail, adminPassword)
	p.waitGone("#login-screen")
	p.settleWizard()

	p.run("turn it off", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID),
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
	)

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if !p.visible("#maintenance-banner") {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("the notice is still shown after maintenance mode was turned off: %q",
		p.text("#maintenance-banner"))
}

// The sign-in screen says who may still come in.
//
// The notice above it is whatever the administrator typed - "back at 14:00" -
// which says nothing about why this particular password was refused, so somebody
// tries it again.
func TestTheSignInScreenSaysWhoMaySignInDuringMaintenance(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	p.run("go out of service",
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("confirm", p.click(".confirm-overlay button:not(.secondary)"))
	p.waitForText("#toast", "out of service")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-maintenance-who") {
		t.Fatal("the sign-in screen does not say who may sign in while the " +
			"installation is out of service")
	}

	said := strings.ToLower(p.text("#login-maintenance-who"))
	if !strings.Contains(said, "admin") {
		t.Errorf("the sign-in screen says %q, without naming who may still come in",
			p.text("#login-maintenance-who"))
	}
}

// The administration screen no longer claims the built-in account is alone.
//
// Everyone who may administer the installation can work during maintenance -
// that is what the server does - and the sentence beside the switch said the
// opposite, which is the kind of wrong that gets believed.
func TestTheMaintenanceCardNamesEveryoneWhoMayWork(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	said := p.text(`#form-maintenance [data-i18n="maint.who"]`)

	if strings.Contains(said, "including other administrators") {
		t.Errorf("the maintenance card still turns other administrators away: %q", said)
	}

	if !strings.Contains(strings.ToLower(said), "administrator role") {
		t.Errorf("the maintenance card does not say the role may work: %q", said)
	}
}
