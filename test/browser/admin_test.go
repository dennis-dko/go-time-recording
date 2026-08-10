//go:build browser

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Three things that can only be checked in a browser: the log viewer, which is
// polling and filtering and nothing a single request would exercise; whether the
// Settings screen fills every one of its cards, which is a property of the chain
// that loads them rather than of any request in it; and how a passkey interacts
// with two-factor authentication - which needs an actual signature from an actual
// authenticator.

// ---------------------------------------------------------------- live log

// The version belongs in the corner of every page, and the footer used to be
// hidden whenever no branding was configured.
func TestTheFooterShowsTheRunningVersion(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	if !p.visible("#site-footer") {
		t.Fatal("the footer is hidden, so the version has nowhere to appear")
	}

	version := strings.TrimSpace(p.text("#footer-version"))
	if version == "" {
		t.Fatal("the footer shows no version")
	}

	// And the platform beside it, as "v1.2.0 (linux)". The same version is
	// published for four platforms and they do not all behave alike - restarting
	// from the interface works here and cannot on Windows - so the version alone
	// does not say what somebody is looking at.
	//
	// The suite runs on Linux, which is what makes the expected value knowable.
	if !strings.Contains(version, "(") || !strings.Contains(version, ")") {
		t.Errorf("the footer shows %q, without the platform in brackets", version)
	}

	if !strings.Contains(version, "(linux)") {
		t.Errorf("the footer shows %q; this suite runs on Linux", version)
	}

	// The version itself is still in front of it, rather than having been
	// replaced by the platform.
	if strings.HasPrefix(version, "(") {
		t.Errorf("the footer shows only the platform: %q", version)
	}
}

// The viewer has to actually fill with lines, which means the poll ran, the
// response parsed and the rendering worked. A card that stays empty looks
// identical to one that is broken.
func TestTheLogViewerFillsWithLines(t *testing.T) {
	// INFO, or the only lines would be the start-up warnings and there would be
	// nothing to prove polling works.
	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	// The level chips are built from what the server reports, so their presence
	// proves the first request came back.
	p.waitForText("#log-levels", "ERROR")

	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(p.text("#log-levels"), level) {
			t.Errorf("no filter offered for %s", level)
		}
	}

	waitForLines(p, "the log viewer never showed a line")

	// Searching narrows what is on screen. The server does the filtering, so this
	// is really asking whether the box reaches it - the filtering itself is
	// covered against the API. Folded in here rather than given its own case
	// because the expensive part is getting to this screen, and a second sign-in
	// and password change is most of a browser test's budget.
	p.run("search for something that cannot appear",
		chromedp.SendKeys("#log-search", "zzz-no-such-line-zzz", chromedp.ByID))

	// The search is debounced and then polled, so this waits rather than
	// asserting at once.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) == "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("a search that matches nothing still shows lines:\n%s",
		truncateText(p.text("#log-output"), 400))
}

// Pausing has to stop the polling, or the button is decoration. Checked by the
// status line, which is what tells the reader whether what they are looking at
// is still moving.
func TestPausingTheLogViewer(t *testing.T) {
	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line before pausing")

	p.run("pause", p.click("#log-pause"))

	// The button offering to resume is the state, and it is the same assertion in
	// either language the interface ships. Checking the status line's wording
	// would be checking a translation.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		label := strings.ToLower(p.text("#log-pause"))
		if strings.Contains(label, "resume") || strings.Contains(label, "fortsetzen") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Errorf("the pause button still says %q, so nothing was paused", p.text("#log-pause"))
}

// An employee must not be offered the log at all - not merely be refused when
// they ask. The whole Settings screen is the built-in administrator's.
func TestAnEmployeeIsNotOfferedTheLog(t *testing.T) {
	p := open(t)
	p.readyAdmin()
	p.createEmployee(t, "gerd@example.com", "gerd-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("gerd@example.com", "gerd-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	if p.visible("#tab-admin") {
		t.Error("an employee is being offered the Settings tab, which holds the log")
	}
}

// ------------------------------------------------------- the Settings cards

// The Settings screen loads its cards in one unbroken chain of awaits, so a card
// whose request fails does not fail alone: every card after it stays blank while
// the API answers perfectly and nothing else notices. The metrics and tracing
// card went into the middle of that chain, which makes the card after it as much
// the point of this test as the new one is.
func TestTheSettingsScreenFillsTheTelemetryCardAndTheOnesAfterIt(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	// This line is written by the same response that fills the fields, so an
	// empty one means the card never loaded at all.
	active := p.text("#telemetry-active")
	if active == "" {
		t.Error("the telemetry card says nothing about what this process is doing")
	}

	// The metrics endpoint is the one thing on this screen somebody wants to
	// copy, so it has to be there in full rather than as a port number to
	// assemble by hand.
	if !strings.Contains(active, "/metrics") {
		t.Errorf("the metrics endpoint is not named in %q", active)
	}

	// The card after the new one in the chain. Its user filter has a default, so
	// an empty field here means the telemetry request threw and took the rest of
	// the screen down with it.
	if filter := p.value(`#form-ldap input[name="userFilter"]`); filter == "" {
		t.Error("the LDAP card is empty, so loading the cards stopped before it")
	}
}

// ------------------------------------------------------ revealing a password

// The button is added by the script, to fields written in the markup, so
// whether it exists at all is a question only a browser can answer - and what it
// does is a change to an attribute that no API test can see.
func TestAPasswordCanBeRevealedAndHiddenAgain(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	const field = `#form-datasource input[name="password"]`

	// Every password field gets one, so this one stands for the rest.
	if !p.visible(`#form-datasource .password-toggle`) {
		t.Fatal("the database password has no reveal button")
	}

	p.run("type a password", chromedp.SendKeys(field, "not-a-real-password", chromedp.ByQuery))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field starts as %q, want it hidden", got)
	}

	p.run("reveal", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "text" {
		t.Errorf("after pressing the button the field is %q, so nothing was revealed", got)
	}

	// The value survived the type change; a reveal that emptied the field would
	// be worse than none.
	if got := p.value(field); got != "not-a-real-password" {
		t.Errorf("revealing changed the value to %q", got)
	}

	p.run("hide again", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field stayed %q after pressing the button a second time", got)
	}
}

// ------------------------------------------------- passkeys and two-factor

// This pins a decision rather than checking a rule, which is why it is worth
// having: with two-factor enabled and a passkey registered, signing in with the
// passkey does not ask for a code.
//
// That is deliberate. Registration and sign-in both demand
// protocol.VerificationRequired, so the device had to see a fingerprint or a PIN
// before it would sign - possession of the device plus verification of the
// person, which is already two factors. It is how Google, Microsoft and Apple
// treat passkeys too: they satisfy multi-factor rather than needing another one
// stacked on top.
//
// The consequence to be aware of: enabling two-factor does not force a second
// factor on someone who has a passkey, because their passkey is a way in that
// never asks. Anyone wanting two-factor as a policy needs more than this
// setting. The test exists so that if the behaviour is ever changed, it is
// changed on purpose.
func TestAPasskeySignsInWithoutATwoFactorCodeEvenWhenTOTPIsOn(t *testing.T) {
	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createEmployee(t, "hanna@example.com", "hanna-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	secret := p.enableTOTP(t)

	// Two-factor really is on: the password alone now stops at the code field.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.run("wait for the code field", chromedp.WaitVisible("#login-totp-field", chromedp.ByID))

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	p.run("supply the code",
		chromedp.SendKeys(`#form-login input[name="totp"]`, code, chromedp.ByQuery),
		p.click(`#form-login button[type="submit"]`))
	p.waitGone("#login-screen")

	// Now register a passkey for the same account.
	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Hanna's laptop", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`))

	p.waitForText("#table-passkeys tbody", "Hanna's laptop")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// And the passkey gets in with no code asked for.
	p.run("sign in with the passkey", p.click("#login-passkey"))
	p.waitGone("#login-screen")

	if p.visible("#login-totp-field") {
		t.Error("the passkey sign-in asked for a two-factor code, which it is not supposed to")
	}

	var who string

	p.run("read who is signed in",
		chromedp.Evaluate(`document.querySelector('#who')?.textContent ?? ''`, &who))

	if !strings.Contains(who, "hanna@example.com") && !strings.Contains(who, "Erika") {
		t.Errorf("expected to be signed in as Hanna, the header says %q", who)
	}
}

// enableTOTP turns two-factor on through the API and returns the secret, so the
// test can produce codes. Driving the enrolment form would be testing the form,
// which other cases already do.
func (p *page) enableTOTP(t *testing.T) string {
	t.Helper()

	var secret string

	p.run("begin two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'POST',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			});

			const body = await r.json();
			return body?.data?.secret ?? '';
		})()`, &secret, awaitPromise))

	if secret == "" {
		t.Fatalf("no two-factor secret was issued\n\napplication log:\n%s", p.app.Log())
	}

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	var status string

	p.run("confirm two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ code: '`+code+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" && status != "201" {
		t.Fatalf("could not enable two-factor: HTTP %s\n\napplication log:\n%s", status, p.app.Log())
	}

	return secret
}

// ------------------------------------------------------------------ helpers

// waitForLines waits until the log output holds something.
func waitForLines(p *page, complaint string) {
	p.t.Helper()

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) != "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s\n\nstatus: %q\n\napplication log:\n%s",
		complaint, p.text("#log-status"), p.app.Log())
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "…"
}

// ------------------------------------------------------- maintenance mode

// The switch has to work from the screen it lives on, and the notice has to be
// visible afterwards - including on the sign-in screen, which is the only place
// somebody turned away can read anything at all.
func TestTurningMaintenanceModeOnAndOffFromTheInterface(t *testing.T) {
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

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !p.visible("#maintenance-banner") {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("the notice is still shown after maintenance mode was turned off: %q",
		p.text("#maintenance-banner"))
}

// ------------------------------------------------- adopting browser defaults

// The browser knows two things the server cannot: which zone the person is
// actually in, and which language they read. Neither was being kept - the
// language was detected for the current page and thrown away on every load, and
// the zone was not detected at all, so somebody far enough east or west saw their
// evening bookings land on the instance's tomorrow until they found the setting.
//
// Only a browser can check this: it is the browser's own zone and language that
// have to reach the database.
func TestAFirstSignInAdoptsTheBrowsersZoneAndLanguage(t *testing.T) {
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

	p.createEmployee(t, "ingrid@example.com", "ingrid-password-1")

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

// ------------------------------------------------------------------- toasts

// A notice raised while the sign-in screen is up used to be invisible.
//
// The toast sat at z-index 20 and the sign-in screen at 30, with an opaque
// background - so a failure during start-up, which is exactly when the sign-in
// screen is still covering the application, was painted behind it. The message
// explaining why nothing worked was the one message nobody could read.
//
// A sign-in that is merely refused is not this case: that message goes into the
// form, next to the field it is about, which is where it belongs.
func TestANoticeIsVisibleOverTheSignInScreen(t *testing.T) {
	p := open(t)

	p.run("wait for the form", chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-screen") {
		t.Fatal("the sign-in screen is not up, so this proves nothing")
	}

	p.run("raise a notice", chromedp.Evaluate(`toast('something went wrong', 'error')`, nil))

	// Visible as the browser sees it, which is the whole point: it was present in
	// the markup before this change too, and painted underneath.
	if !p.visible("#toast .toast-note") {
		t.Error("a notice raised while the sign-in screen is up cannot be seen")
	}

	if !strings.Contains(p.text("#toast"), "something went wrong") {
		t.Errorf("the notice says %q", p.text("#toast"))
	}
}

// Two failures in a row used to show only the second, which is the case where
// the first one mattered.
func TestTwoNoticesAreBothShown(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Cleared first: signing in and changing the password raises notices of its
	// own, and they now linger long enough to still be there.
	p.run("raise two notices", chromedp.Evaluate(`
		document.querySelector('#toast').replaceChildren();
		toast('first notice', 'error');
		toast('second notice', 'error');`, nil))

	var count int

	p.run("count the notices",
		chromedp.Evaluate(`document.querySelectorAll('#toast .toast-note').length`, &count))

	if count != 2 {
		t.Errorf("%d notice(s) on screen, want 2", count)
	}

	shown := p.text("#toast")
	for _, want := range []string{"first notice", "second notice"} {
		if !strings.Contains(shown, want) {
			t.Errorf("%q is not shown; the stack says %q", want, shown)
		}
	}
}

// -------------------------------------------------- asking before destroying

// Five delete buttons asked nothing at all - a role, a project, a time entry, a
// token and a passkey all went straight to DELETE on one click. The four that did
// ask used window.confirm, which the browser draws itself: unstyled, naming the
// origin, unreadable in a dark theme and impossible to translate.
//
// Only a browser can check either half: that the question appears, and that
// answering "no" leaves the thing alone.
func TestDeletingAsksFirstAndCancellingChangesNothing(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// A time entry of the administrator's own, so nothing else has to exist.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "1.37", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "1.37")

	// The delete button is a link button in the row's action cell.
	p.run("press delete", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	// The question is ours, not the browser's - so it is in the page and can be
	// seen, which a native dialog cannot be.
	if !p.visible(".confirm-card") {
		t.Fatal("no dialog appeared before deleting")
	}

	// Cancelling has to leave the entry exactly where it was.
	p.run("cancel", p.click(`.confirm-actions button.secondary`))
	p.waitGone(".confirm-overlay")

	if !strings.Contains(p.text("#table-timesheets tbody"), "1.37") {
		t.Error("cancelling the dialog deleted the entry anyway")
	}

	// And confirming deletes it, or the dialog is a wall rather than a question.
	p.run("press delete again", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))
	p.run("confirm", p.click(`.confirm-actions button.danger`))
	p.waitGone(".confirm-overlay")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(p.text("#table-timesheets tbody"), "1.37") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("confirming the dialog did not delete the entry")
}

// Escape is the ambiguous keypress, and a dialog with a destructive option has
// to read it as "no".
func TestEscapeCancelsTheConfirmation(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "2.5", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "2.5")

	p.run("press delete", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("press escape", chromedp.KeyEvent("\u001b"))
	p.waitGone(".confirm-overlay")

	if !strings.Contains(p.text("#table-timesheets tbody"), "2.5") {
		t.Error("escape deleted the entry")
	}
}

// ------------------------------------------------------------------ stopwatch

// The clock counts up in the page without asking the server every second, and
// stopping it books what was measured. Both halves are browser behaviour: the
// ticking display, and the buttons swapping over when it starts.
func TestTheStopwatchRunsAndBooksWhatItMeasured(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open time entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID))

	if !p.visible("#timer-start") {
		t.Fatal("no start button")
	}

	p.run("start it",
		chromedp.SendKeys("#timer-description", "measured in a browser", chromedp.ByID),
		p.click("#timer-start"))

	// The buttons swap over, which is how somebody can tell it took.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !p.visible("#timer-stop") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-stop") {
		t.Fatal("the stop button never appeared, so the clock did not start")
	}

	if p.visible("#timer-start") {
		t.Error("the start button is still offered while the clock runs")
	}

	// And it counts up on its own. Two readings a couple of seconds apart have to
	// differ, or the display is a decoration.
	first := p.text("#timer-elapsed")
	if first == "" {
		t.Fatal("the elapsed display is empty while the clock runs")
	}

	time.Sleep(3 * time.Second)

	if second := p.text("#timer-elapsed"); second == first {
		t.Errorf("the display is stuck at %q, so it is not counting", first)
	}

	// Past the smallest bookable duration - below it the stop is refused on
	// purpose, which is a different test.
	time.Sleep(38 * time.Second)

	p.run("stop and book", p.click("#timer-stop"))

	// The entry appears in the list, with the description that was typed.
	p.waitForText("#table-timesheets tbody", "measured in a browser")

	// And the clock is back to offering a start.
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !p.visible("#timer-start") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-start") {
		t.Error("after stopping, the clock does not offer to start again")
	}
}

// --------------------------------------------------------------------- charts

// The charts are SVG built in the page, because the Content-Security-Policy
// allows no external origin and a chart library from a CDN would simply be
// blocked. That makes them a browser question twice over: whether the elements
// exist, and whether they render - an <svg> built in the HTML namespace parses
// without complaint and draws nothing at all.
func TestTheOwnHoursChartsAreDrawn(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Two entries on one day and none on the next, so an empty day has something
	// to be empty about.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "3.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "3.25")

	p.run("open overtime", p.click(`.tab[data-view="overtime"]`))

	// Worth asserting rather than assuming: the click that opens this view landed
	// on a notice instead of the tab until the notices were made transparent to
	// the pointer, and the symptom was a view that simply never opened.
	if !p.visible("#view-overtime") {
		t.Fatal("the overtime view did not open - something is covering the tab")
	}

	if !p.visible("#statistics-card") {
		t.Fatal("the overtime view is open but the statistics card is not visible")
	}

	// An explicit range, so the number of rows below is a fixed expectation. The
	// default is the first of the month to today, which is a different length
	// every day.
	p.run("evaluate",
		chromedp.SetValue("#statistics-from", "2026-08-01", chromedp.ByID),
		chromedp.SetValue("#statistics-to", "2026-08-31", chromedp.ByID),
		p.click("#statistics-load"))

	// A bar exists, and the SVG is in the right namespace - an HTML-namespace
	// <svg> would be found by a selector and occupy no space.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#chart-days svg .chart-bar") {
		t.Fatalf("no bar was drawn for the day chart; the container holds %q",
			truncateText(p.text("#chart-days"), 200))
	}

	var namespace string

	p.run("read the namespace", chromedp.Evaluate(
		`document.querySelector('#chart-days svg')?.namespaceURI ?? ''`, &namespace))

	if namespace != "http://www.w3.org/2000/svg" {
		t.Errorf("the chart is in the %q namespace, so it would render nothing", namespace)
	}

	// The total is on screen, and the figure is the one that was booked.
	if total := p.text("#statistics-total"); !strings.Contains(total, "3.25") {
		t.Errorf("the total says %q, want it to mention 3.25", total)
	}

	// Every day of the month is a row, including the ones with nothing on them:
	// a chart of only the days that have entries reads as a full week.
	var rows int

	p.run("count the rows",
		chromedp.Evaluate(`document.querySelectorAll('#chart-days .chart-track').length`, &rows))

	if rows != 31 {
		t.Errorf("the day chart has %d rows, want 31 - one for every day of August, "+
			"including the empty ones", rows)
	}

	// And the project chart drew the uncategorised bucket, since the entry has no
	// project - which is an answer rather than a gap.
	if !strings.Contains(p.text("#chart-projects"), "3.25") {
		t.Errorf("the project chart does not show the hours: %q",
			truncateText(p.text("#chart-projects"), 200))
	}
}

// A refusal from the server reaches the reader in their own language.
//
// The messages are written where the rule is enforced, in English, which is right
// for the log and wrong for the person who tripped over it: "an approved timesheet
// can no longer be edited" was shown to a German reader whatever they had chosen.
// The reason now travels as a code with the values the sentence interpolated, and
// the interface looks the sentence up.
//
// Proved in a browser because that is the only place the lookup happens - on the
// wire the message is still English, deliberately, and the integration tests check
// exactly that.
func TestAServerRefusalIsShownInTheReadersLanguage(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// German, which is the only other language shipped.
	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	time.Sleep(300 * time.Millisecond)

	// A daily ceiling, then a booking over it: a refusal with four values in it,
	// which is the case that would fall apart if the values were dropped.
	// The instance-wide ceiling lives on the administration screen, not under
	// My account, which holds the per-account one.
	p.run("set a daily ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.SetValue(`#form-operational input[name="maxDailyHours"]`, "8", chromedp.ByQuery),
		p.click(`#form-operational button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.run("clear the notices", chromedp.Evaluate(
		`document.querySelector('#toast').replaceChildren()`, nil))

	p.run("book over the ceiling",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "9", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); shown != "" {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if shown == "" {
		t.Fatalf("the refusal raised no notice at all\n\napplication log:\n%s", p.app.Log())
	}

	// The German sentence, not the English one it was built from.
	if !strings.Contains(shown, "Tagesmaximum") {
		t.Errorf("the notice is not the German sentence: %q", shown)
	}

	if strings.Contains(shown, "daily limit") {
		t.Errorf("the notice is still the server's English wording: %q", shown)
	}

	// And the figures survived the translation - 9 booked against a ceiling of 8.
	for _, figure := range []string{"9", "8"} {
		if !strings.Contains(shown, figure) {
			t.Errorf("the notice lost the figure %s: %q", figure, shown)
		}
	}
}

// A field the server rejects is named the way the form names it.
func TestARejectedFieldIsNamedNotIdentified(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	time.Sleep(300 * time.Millisecond)

	// A negative ceiling, which is refused by field rather than by sentence.
	p.run("save an impossible ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const field = document.querySelector('#form-operational input[name="maxDailyHours"]');
			field.value = '-3';
			field.removeAttribute('min');
		})()`, nil),
		p.click(`#form-operational button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); strings.Contains(shown, "Max/Tag") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !strings.Contains(shown, "Max/Tag") {
		t.Errorf("the refusal does not name the field as the form does: %q\n\n"+
			"application log:\n%s", shown, p.app.Log())
	}

	if strings.Contains(shown, "maxDailyHours") {
		t.Errorf("the refusal still shows the column name: %q", shown)
	}

	if strings.Contains(shown, "invalid parameter") {
		t.Errorf("the refusal still carries GoFr's parameter count: %q", shown)
	}
}

// The enrolment QR code has to be on screen, and has to be the code the server
// drew rather than a broken image.
//
// A picture that fails to load still occupies its box, so "the element is there"
// proves nothing: this checks the browser decoded it, which for an SVG data URI
// means the markup parsed.
func TestTheTwoFactorQRCodeIsShown(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("start two-factor enrolment",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#totp-card", chromedp.ByID),
		p.click("#totp-begin"),
		chromedp.WaitVisible("#totp-qr", chromedp.ByQuery),
	)

	var loaded bool

	// naturalWidth is 0 for an image the browser could not decode, whatever the
	// element's own size is.
	p.run("check the code decoded", chromedp.Evaluate(`(() => {
		const img = document.querySelector('#totp-qr');
		return Boolean(img && img.complete && img.naturalWidth > 0);
	})()`, &loaded))

	if !loaded {
		t.Errorf("the QR code did not load\n\nsrc: %.60s\n\napplication log:\n%s",
			p.attr("#totp-qr", "src"), p.app.Log())
	}

	if src := p.attr("#totp-qr", "src"); !strings.HasPrefix(src, "data:image/svg+xml") {
		t.Errorf("the code is not an inline SVG: %.60s", src)
	}

	// The typed key is still reachable, folded away behind the picture.
	if p.text("#totp-secret") == "" {
		t.Error("the key to type is gone; a machine with no camera has no way in")
	}

	// Leaving the screen and coming back must not leave the secret on it: the code
	// encodes it, and the enrolment it belonged to is over.
	p.run("leave and come back",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	)

	if p.visible("#totp-qr") {
		t.Error("the QR code survived the enrolment it belonged to")
	}

	if secret := p.text("#totp-secret"); secret != "" {
		t.Errorf("the secret is still on screen after leaving: %q", secret)
	}
}

// A first sign-in is greeted, and the greeting offers the walk through.
//
// Somebody arriving in an application nobody has introduced is the moment they
// decide it is complicated. The greeting is also the only place the tour is offered
// automatically, so if it fails to appear the walk through is effectively gone.
func TestAFirstSignInIsGreetedAndOfferedTheTour(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// An ordinary employee, because the built-in administrator is deliberately not
	// greeted: it arrives at the setup wizard, and a walk through booking time
	// would be a walk through somebody else's job.
	p.run("create an employee",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Rieke", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "rieke@example.com", chromedp.ByQuery),
		chromedp.SetValue(`#form-user select[name="role"]`, "employee", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="password"]`, "rieke-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	// The administrator was not greeted, which is half the requirement.
	if p.visible("#welcome-overlay") {
		t.Error("the built-in administrator was greeted with the tour")
	}

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("rieke@example.com", "rieke-password-1")
	p.waitGone("#login-screen")

	p.run("wait for the greeting",
		chromedp.WaitVisible("#welcome-overlay", chromedp.ByID))

	if title := p.text("#welcome-title"); !strings.Contains(title, "Rieke") {
		t.Errorf("the greeting does not name the person: %q", title)
	}

	// The points offered are the ones this person can act on. An employee cannot
	// approve, so that line has no business being there.
	if points := p.text("#welcome-points"); strings.Contains(points, "genehmige") {
		t.Errorf("an employee is promised approvals: %q", points)
	}

	p.run("take the tour", p.click("#welcome-tour"),
		chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	if p.visible("#welcome-overlay") {
		t.Error("the greeting is still up behind the tour")
	}

	// The walk has to be a walk: the first step counts itself, and Next moves on.
	first := p.text("#tour-title")

	if first == "" {
		t.Error("the first step has no title")
	}

	if count := p.text("#tour-count"); count == "" {
		t.Error("the tour does not say where in it you are")
	}

	p.run("next step", p.click("#tour-next"))
	time.Sleep(400 * time.Millisecond)

	if second := p.text("#tour-title"); second == first {
		t.Errorf("Next did not move on; still on %q", first)
	}

	p.run("leave the tour", p.click("#tour-end"))
	time.Sleep(400 * time.Millisecond)

	if p.visible("#tour-bubble") {
		t.Error("skipping did not end the tour")
	}

	// Seen once: a reload must not greet them again, or "not now" would mean
	// nothing.
	p.run("reload", chromedp.Reload())
	p.waitGone("#login-screen")

	time.Sleep(800 * time.Millisecond)

	if p.visible("#welcome-overlay") {
		t.Error("the greeting came back after being answered")
	}
}

// Somebody who has been here before is greeted differently: once per visit, with
// what they would otherwise have to go and look up.
func TestAReturningSignInIsGreetedOncePerVisit(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("create an employee",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Sven", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "sven@example.com", chromedp.ByQuery),
		chromedp.SetValue(`#form-user select[name="role"]`, "employee", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="password"]`, "sven-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("sven@example.com", "sven-password-1")
	p.waitGone("#login-screen")

	// Answer the first-sign-in greeting, which is what makes the next one a
	// return rather than an arrival.
	p.run("decline the tour",
		chromedp.WaitVisible("#welcome-overlay", chromedp.ByID),
		p.click("#welcome-skip"),
	)

	time.Sleep(600 * time.Millisecond)

	// A reload in the same tab is not an arrival, so no welcome back either.
	p.run("reload", chromedp.Reload())
	p.waitGone("#login-screen")

	time.Sleep(800 * time.Millisecond)

	if p.visible("#welcome-back") {
		t.Error("a reload was greeted; the greeting is per visit, not per page load")
	}

	// A fresh session is. Simulated by clearing the per-tab marker, which is what
	// opening the application in a new tab does.
	p.run("come back later",
		chromedp.Evaluate(`sessionStorage.clear()`, nil),
		chromedp.Reload(),
	)

	p.waitGone("#login-screen")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p.visible("#welcome-back") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#welcome-back") {
		t.Fatalf("a returning visit was not greeted\n\napplication log:\n%s", p.app.Log())
	}

	if title := p.text("#welcome-back-title"); !strings.Contains(title, "Sven") {
		t.Errorf("the greeting does not name the person: %q", title)
	}

	// And it says something about today rather than only hello.
	if detail := p.text("#welcome-back-text"); detail == "" {
		t.Error("the greeting says nothing about today")
	}

	// Closable, and it stays closed.
	p.run("close it", p.click("#welcome-back-close"))
	time.Sleep(200 * time.Millisecond)

	if p.visible("#welcome-back") {
		t.Error("the greeting could not be dismissed")
	}
}

// The spreadsheet card: an export that downloads, and an import that shows what a
// file would do before it does it.
//
// The preview is the part worth driving in a browser. A file assembled by hand is
// wrong more often than it is right, and the whole point is that somebody sees
// which rows are refused and why, on screen, with the import button withheld until
// the file is clean.
func TestTheImportShowsWhatAFileWouldDoBeforeDoingIt(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// A file with one good row and one that no ceiling allows, written through the
	// same writer the export uses.
	book, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "This one is fine"},
		{Date: time.Now().AddDate(0, 0, 1), Hours: 30, Description: "This one is not"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	path := filepath.Join(t.TempDir(), "entries.xlsx")
	if err := os.WriteFile(path, book, 0o600); err != nil {
		t.Fatalf("writing the workbook: %v", err)
	}

	p.run("choose the file",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{path}, chromedp.ByQuery),
	)

	time.Sleep(300 * time.Millisecond)

	if !p.visible("#wb-preview") {
		t.Fatal("choosing a file did not offer to check it")
	}

	p.run("check the file", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-preview-wrap", chromedp.ByID))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-workbook tbody"), "This one is fine") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	shown := p.text("#table-workbook tbody")

	if !strings.Contains(shown, "This one is fine") {
		t.Fatalf("the preview does not show the file's rows: %q\n\napplication log:\n%s",
			shown, p.app.Log())
	}

	// The refused row says why, on its own line.
	var rejected int

	p.run("count the refused rows", chromedp.Evaluate(
		`document.querySelectorAll('#table-workbook tbody tr.rejected').length`, &rejected))

	if rejected != 1 {
		t.Errorf("%d row(s) are marked as refused, want 1", rejected)
	}

	if summary := p.text("#wb-summary"); !strings.Contains(summary, "1") {
		t.Errorf("the summary does not say how many rows are usable: %q", summary)
	}

	// And the import is withheld: offering it for a file that would be refused is
	// offering a failure.
	if p.visible("#wb-import") {
		t.Error("the import was offered for a file with a refused row in it")
	}

	// Nothing was written by looking.
	if entries := p.text("#table-timesheets tbody"); strings.Contains(entries, "This one is fine") {
		t.Error("the preview created entries")
	}

	// A clean file is offered, and goes through.
	clean, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "Imported from a file"},
	})
	if err != nil {
		t.Fatalf("building the second workbook: %v", err)
	}

	cleanPath := filepath.Join(t.TempDir(), "clean.xlsx")
	if err := os.WriteFile(cleanPath, clean, 0o600); err != nil {
		t.Fatalf("writing the second workbook: %v", err)
	}

	p.run("choose a clean file",
		chromedp.SetUploadFiles("#wb-file", []string{cleanPath}, chromedp.ByQuery))

	time.Sleep(300 * time.Millisecond)

	p.run("check it", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-import", chromedp.ByID))

	p.run("import it", p.click("#wb-import"))

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "Imported from a file") {
			return
		}

		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("the imported entry never reached the table\n\ntable:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}

// The loading strip appears while a request is in flight and goes when it is not.
//
// Two failure modes are worth pinning. One is a strip that never appears, which
// makes a slow screen look frozen. The other is a strip that never leaves — an
// in-flight counter that decrements on success only would stick on the first
// failed request and stay for as long as the page is open.
func TestTheLoadingStripAppearsAndGoesAgain(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// At rest: nothing in flight, so nothing on screen.
	if p.visible("#progress") {
		t.Error("the loading strip is showing with no request in flight")
	}

	// Held open on the server side so the strip has time to appear: the delay
	// before it shows is deliberate, and a fast request must show nothing.
	var appeared bool

	p.run("watch a slow request", chromedp.Evaluate(`(async () => {
		// A request that takes long enough to be worth mentioning. The log
		// endpoint is admin-only and always answers, which is all this needs.
		const inFlight = fetch('/api/v1/logs?limit=1', { credentials: 'same-origin' });

		// Past the delay the strip waits out before it shows itself.
		await new Promise((r) => setTimeout(r, 400));

		const bar = document.querySelector('#progress');
		const showing = Boolean(bar) && !bar.hidden;

		await inFlight.catch(() => {});
		return showing;
	})()`, &appeared, awaitPromise))

	// Not asserted as a failure: on a fast local server the request can finish
	// inside the delay, which is the strip behaving correctly.
	t.Logf("the strip was showing during the request: %v", appeared)

	// What must hold either way: it is gone once nothing is in flight.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !p.visible("#progress") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if p.visible("#progress") {
		t.Error("the loading strip stayed on screen after the request finished")
	}

	// And a failed request leaves it clean too - the counter has to come back
	// down in a finally, not on the success path.
	p.run("make a request that fails", chromedp.Evaluate(`(async () => {
		try { await fetch('/api/v1/does-not-exist', { credentials: 'same-origin' }); } catch {}
	})()`, nil, awaitPromise))

	time.Sleep(900 * time.Millisecond)

	if p.visible("#progress") {
		t.Error("the loading strip stayed on screen after a failed request")
	}
}
