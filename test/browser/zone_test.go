//go:build browser

package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The browser knows two things the server cannot: which zone the person is
// actually in, and which language they read. Neither was being kept - the
// language was detected for the current page and thrown away on every load, and
// the zone was not detected at all, so somebody far enough east or west saw their
// evening bookings land on the instance's tomorrow until they found the setting.
//
// Only a browser can check this: it is the browser's own zone and language that
// have to reach the database.
func TestAFirstSignInAdoptsTheBrowsersZoneAndLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// An instance zone that is not this machine's, which is the case the feature
	// exists for - somebody working in a different zone from the installation.
	// The wizard sets the instance to Europe/Berlin, and adopting a zone that
	// already applies would change nothing, so without this the assertion below
	// would be about a no-op.
	p.run("move the instance to another zone", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
			await fetch('/api/v1/settings/timezone', {
				method: 'PUT', credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ timezone: 'Pacific/Auckland' }),
			});
		})()`, nil, awaitPromise))

	p.createOrdinaryAccount(t, "ingrid@example.com", "ingrid-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("ingrid@example.com", "ingrid-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	// Read back from the server rather than from the form: the point is that it
	// was written to the database, not that a select was filled in.
	stored := p.storedAccount(t)

	if stored.Timezone == "" {
		t.Error("the browser's zone was not adopted, so the account still follows the instance")
	}

	// Whatever the machine running this test is set to - asserting a specific
	// zone would be asserting about the test machine.
	if browser := p.browserTimezone(); stored.Timezone != browser {
		t.Errorf("the account has %q, want the browser's %q", stored.Timezone, browser)
	}

	if stored.Language == "" {
		t.Error("the browser's language was not adopted")
	}

	// And it is a suggestion rather than a standing override: choosing to follow
	// the instance has to survive a reload, or the setting would be unusable.
	p.run("follow the instance again", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
			await fetch('/api/v1/me/timezone', {
				method: 'PUT', credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ timezone: '' }),
			});
		})()`, nil, awaitPromise))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")

	if again := p.storedAccount(t); again.Timezone != "" {
		t.Errorf("following the instance was overwritten again with %q", again.Timezone)
	}
}

// storedAccount reads /me, so an assertion is about what the server holds rather
// than about what a form shows.
func (p *page) storedAccount(t *testing.T) struct {
	Timezone string
	Language string
} {
	t.Helper()

	var raw string

	p.run("read /me", chromedp.Evaluate(`
		fetch('/api/v1/me', { credentials: 'same-origin' })
			.then(r => r.json())
			.then(b => JSON.stringify({
				timezone: b.data?.user?.timezone ?? '',
				language: b.data?.user?.language ?? '',
			}))`, &raw, awaitPromise))

	var account struct {
		Timezone string `json:"timezone"`
		Language string `json:"language"`
	}

	if err := json.Unmarshal([]byte(raw), &account); err != nil {
		t.Fatalf("cannot read /me: %v (%s)", err, truncateText(raw, 200))
	}

	return struct {
		Timezone string
		Language string
	}{Timezone: account.Timezone, Language: account.Language}
}

// browserTimezone is what the browser reports for itself, which is what the
// adoption is supposed to have stored.
func (p *page) browserTimezone() string {
	p.t.Helper()

	var zone string

	p.run("read the browser zone",
		chromedp.Evaluate(`Intl.DateTimeFormat().resolvedOptions().timeZone || ''`, &zone))

	return zone
}

// The way back to the instance's zone says which zone that is.
//
// The option named whatever was in effect for the account, which is the same
// thing only until somebody chooses a zone of their own. After choosing
// Africa/Abidjan by hand, the line offering to stop choosing read "follow the
// instance setting (Africa/Abidjan)" - an offer to change nothing, with no way
// to find out what stopping would actually give you.
func TestFollowingTheInstanceZoneNamesTheInstanceZone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// A zone for the installation that nobody would pick by accident, and a
	// different one for this account.
	const (
		instanceZone = "Pacific/Auckland"
		ownZone      = "Africa/Abidjan"
	)

	var status string

	p.run("set the instance zone", chromedp.Evaluate(fmt.Sprintf(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/timezone', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ timezone: %q }),
			});

			return String(r.status);
		})()`, instanceZone), &status, awaitPromise))

	if status != "200" {
		t.Fatalf("setting the instance zone answered %s", status)
	}

	// Reloaded, because who is signed in is read once: without this the page
	// still holds the zone the installation had when it was opened.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	p.run("open My account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#my-timezone", chromedp.ByID))

	// Straight to a chosen zone rather than checking the untouched state first.
	// There is no untouched state to check here: the first load stores whatever
	// zone the browser reports, so this account already has one of its own
	// before the screen is ever opened.
	p.run("choose a zone of my own",
		p.chooseOption("#my-timezone", ownZone),
		p.click(`#form-my-timezone button[type="submit"]`))

	p.waitForText("#toast", "saved")

	// The point of the whole case: the way back still names the instance's zone.
	got := p.text(`#my-timezone option[value=""]`)

	if strings.Contains(got, ownZone) {
		t.Errorf("after choosing %s, the way back reads %q - it offers to change "+
			"nothing", ownZone, got)
	}

	if !strings.Contains(got, instanceZone) {
		t.Errorf("the way back reads %q, without naming the instance's zone %s",
			got, instanceZone)
	}
}
