//go:build browser

package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	page_ "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
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

	if version := strings.TrimSpace(p.text("#footer-version")); version == "" {
		t.Error("the footer shows no version")
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

	// window.confirm blocks a headless browser forever, so it is answered
	// automatically. The dialog itself is not what this case is about.
	p.acceptDialogs()

	p.run("turn it on",
		chromedp.SendKeys(`#form-maintenance input[name="message"]`, "Restoring a backup", chromedp.ByQuery),
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
	)

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

// acceptDialogs answers window.confirm and window.alert automatically.
//
// A headless browser has nobody to click them, so an unanswered dialog blocks
// every later action until the test's deadline - which reads as "the click did
// nothing" rather than "something asked a question".
func (p *page) acceptDialogs() {
	p.t.Helper()

	chromedp.ListenTarget(p.ctx, func(event any) {
		if _, ok := event.(*page_.EventJavascriptDialogOpening); ok {
			go func() {
				_ = chromedp.Run(p.ctx, page_.HandleJavaScriptDialog(true))
			}()
		}
	})
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
