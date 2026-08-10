//go:build browser

// Package browser drives the real interface in a real browser.
//
// The integration tests prove the API answers correctly. They cannot prove
// anyone can *use* it: whether the sign-in screen actually goes away, whether a
// tab switch shows the right panel, whether a stylesheet rule quietly beats the
// hidden attribute. Those failures leave the API perfectly healthy and the
// application unusable.
//
// That is not hypothetical here. This project shipped a sign-in form that
// authenticated correctly and then left the overlay on screen, because
// `display: flex` on .login-screen won over the browser's own
// `[hidden] { display: none }`. Every API check passed. Only opening it in a
// browser showed it.
//
//	task test:browser
//	go test -tags browser ./test/browser
//
// Needs Chrome, Chromium or Edge. chromedp finds it; set CHROME_PATH if it is
// somewhere unusual.
package browser

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// interactionTimeout bounds one test. Generous, because a cold browser start
// and the application's first migration both land inside it.
const interactionTimeout = 90 * time.Second

func TestMain(m *testing.M) {
	cleanup, err := harness.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	cleanup()
	os.Exit(code)
}

// page is a browser pointed at a fresh instance.
type page struct {
	t   *testing.T
	ctx context.Context
	app *harness.App
}

// open starts an instance and a browser, and loads the interface.
func open(t *testing.T) *page {
	t.Helper()

	return openWith(t)
}

// openWith is open with extra environment for the instance, for the cases that
// need the application configured differently - the log viewer needs a log level
// that actually produces lines.
func openWith(t *testing.T, env ...string) *page {
	t.Helper()

	app := harness.Start(t, env...)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		// The container images CI uses run as root, where Chrome refuses to
		// start without this.
		chromedp.NoSandbox,
	)

	if path := os.Getenv("CHROME_PATH"); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancelAlloc)

	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)

	ctx, cancelTimeout := context.WithTimeout(ctx, interactionTimeout)
	t.Cleanup(cancelTimeout)

	p := &page{t: t, ctx: ctx, app: app}

	p.run("load the interface",
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#form-login", chromedp.ByID),
	)

	return p
}

// run executes actions and fails the test with the application's log attached,
// which is usually where the reason is.
func (p *page) run(what string, actions ...chromedp.Action) {
	p.t.Helper()

	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v\n\napplication log:\n%s", what, err, p.app.Log())
	}
}

// signIn fills the form and submits it, the way a person does.
func (p *page) signIn(email, password string) {
	p.t.Helper()

	p.run("sign in",
		chromedp.WaitVisible("#form-login", chromedp.ByID),
		chromedp.SendKeys(`#form-login input[name="email"]`, email, chromedp.ByQuery),
		chromedp.SendKeys(`#form-login input[name="password"]`, password, chromedp.ByQuery),
		chromedp.Click(`#form-login button[type="submit"]`, chromedp.ByQuery),
	)
}

// settleWizard completes the setup wizard through the API.
//
// It has to go, not merely be hidden: it is an overlay across the whole page,
// so anything behind it receives no clicks. Hiding it with a line of script
// does not last either - the next refresh puts it back, because the server
// still reports required steps outstanding. That cost an afternoon of "the
// submit button does nothing", which is exactly the failure this package is
// for, just aimed at the test instead of the application.
//
// Through the API rather than by clicking: the wizard has its own test, and
// every other test wants it out of the way rather than exercised again.
func (p *page) settleWizard() {
	p.t.Helper()

	var out string

	p.run("settle the setup wizard", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';')
				.map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const post = (path, body) => fetch(path, {
				method: body === undefined ? 'POST' : 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: body === undefined ? undefined : JSON.stringify(body),
			}).then(r => r.status);

			const results = [];
			results.push('tz=' + await post('/api/v1/settings/timezone', { timezone: 'Europe/Berlin' }));
			results.push('complete=' + await post('/api/v1/setup/complete'));

			document.querySelector('#setup-wizard').hidden = true;
			return results.join(' ');
		})()`, &out, awaitPromise))

	// Every result has to be a 2xx. Checked by parsing rather than by looking
	// for "=4", which also matches "=404" - and did, when an endpoint this
	// helper used to call was removed.
	for _, result := range strings.Fields(out) {
		_, status, _ := strings.Cut(result, "=")

		code, err := strconv.Atoi(status)
		if err != nil || code < 200 || code > 299 {
			p.t.Fatalf("could not settle the wizard: %s\n\napplication log:\n%s", out, p.app.Log())
		}
	}
}

// settleWelcome dismisses the first-sign-in greeting if it is up.
//
// It has to go, not merely be ignored: it is a modal across the whole page, so
// everything behind it receives no clicks - the same trap the setup wizard sets,
// and the reason three passkey tests started timing out on "sign out" the moment
// the greeting was added. The built-in administrator is never greeted, so this
// matters for the ordinary accounts the tests create.
//
// Declining rather than taking the walk: the tour has its own test, and every
// other test wants the screen to itself.
func (p *page) settleWelcome() {
	p.t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if p.visible("#welcome-overlay") {
			p.run("decline the greeting", p.click("#welcome-skip"))
			p.waitGone("#welcome-overlay")

			return
		}

		time.Sleep(150 * time.Millisecond)
	}

	// Not an error: an account that has already been greeted, or one the greeting
	// does not apply to, is a perfectly ordinary state to be in.
}

// awaitPromise makes chromedp wait for an async evaluation to resolve instead
// of handing back the pending promise.
func awaitPromise(ep *runtime.EvaluateParams) *runtime.EvaluateParams {
	return ep.WithAwaitPromise(true)
}

// click scrolls an element clear of the sticky top bar, then clicks it.
//
// chromedp scrolls a target to the top of the viewport, which is where the top
// bar is - so the click lands on a navigation tab instead. Centring it first is
// what a person does by scrolling naturally.
func (p *page) click(selector string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.Run(ctx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(
				`document.querySelector(%q).scrollIntoView({block: 'center'})`, selector), nil),
			chromedp.Sleep(120*time.Millisecond),
			chromedp.Click(selector, chromedp.ByQuery),
		)
	})
}

// readyAdmin signs in, replaces the initial password and settles the wizard,
// leaving an administrator who can actually do things.
//
// All three are needed: the server refuses the rest of the API until the
// password is replaced, and the wizard is an overlay that swallows clicks
// until it is settled. A test that skips either spends its time discovering
// that again.
const adminPassword = "a-much-better-password"

func (p *page) readyAdmin() {
	p.t.Helper()

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")
	p.settleWizard()

	p.run("replace the initial password",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-password", chromedp.ByID),
		chromedp.SendKeys(`#form-password input[name="currentPassword"]`,
			harness.AdminPassword, chromedp.ByQuery),
		chromedp.SendKeys(`#form-password input[name="newPassword"]`,
			adminPassword, chromedp.ByQuery),
		p.click(`#form-password button[type="submit"]`),
	)

	// The session survives the change now: the server ends the other devices and
	// keeps this one, because this is the device that just proved it knew the old
	// password. Waiting for a sign-in screen here would wait for ever.
	time.Sleep(600 * time.Millisecond)

	if p.visible("#login-screen") {
		p.t.Fatalf("changing the password signed the administrator out; the session was meant to survive it")
	}

	p.settleWizard()
}

// workerEmail and workerPassword are the account readyWorker signs in as.
const (
	workerEmail    = "wera@example.com"
	workerPassword = "wera-password-1"
)

// readyWorker signs in as somebody who works here.
//
// The built-in administrator records no time: it exists on every installation before
// anybody has chosen anything, so it is how you get in rather than somebody's working
// day. A case about booking, a calendar, a stopwatch, a chart or a project therefore
// cannot be driven by it - every one of those screens is gated on a right it does not
// hold, and the case would be testing an empty page.
//
// The administrator is still needed first: the initial password has to be replaced
// before anything else answers, and only it can create an account.
func (p *page) readyWorker() {
	p.t.Helper()

	p.readyAdmin()
	p.becomeWorker()
}

// becomeWorker creates an ordinary account and signs in as it, from a page that is
// already signed in as the administrator.
//
// Its own step, because a case sometimes needs both in order: something set on the
// administration screen, and then the working day it applies to.
func (p *page) becomeWorker() {
	p.t.Helper()

	p.createEmployee(p.t, workerEmail, workerPassword)

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")
	p.settleWelcome()
}

// visible reports whether an element is on screen, as the browser sees it -
// not whether it exists in the markup.
func (p *page) visible(selector string) bool {
	p.t.Helper()

	var result bool

	// checkVisibility rather than offsetParent: a position:fixed element always
	// has a null offsetParent, so the obvious check reports the sign-in overlay
	// as hidden while it covers the whole screen. The bounding box is the
	// fallback for browsers without it.
	p.run("check visibility of "+selector, chromedp.Evaluate(fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			if (!el) return false;
			if (typeof el.checkVisibility === 'function') {
				return el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true });
			}
			const style = getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return false;
			const box = el.getBoundingClientRect();
			return box.width > 0 && box.height > 0;
		})()`, selector), &result))

	return result
}

// waitGone polls until an element is no longer visible.
func (p *page) waitGone(selector string) {
	p.t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		if !p.visible(selector) {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.t.Fatalf("%s is still visible after 20s\n\napplication log:\n%s", selector, p.app.Log())
}

func (p *page) text(selector string) string {
	p.t.Helper()

	var out string

	p.run("read "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.textContent ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}

// atRest waits until nothing is in flight and the loading strip has been put away.
//
// Necessary because the screen has just been filled. A strip showing while a request
// really is in flight is the strip doing its job, so asserting "nothing is loading"
// straight after a reload asserts it about a page that is still loading - and it
// passed or failed on how fast the last request happened to be. It failed in CI
// exactly that way.
//
// The invariant worth holding is the one this waits for: no requests outstanding and
// nothing drawn. When it cannot be reached the counter is reported alongside, because
// zero in flight with a strip still on screen is a different fault from a request
// that never came back.
func (p *page) atRest() {
	p.t.Helper()

	// progress is a top-level const, so it is reachable by bare name but is not a
	// property of window. Checked once, so an unreachable counter reads as itself
	// rather than as a ReferenceError from somewhere inside chromedp.
	var kind string

	p.run("look for the progress counter", chromedp.Evaluate(`typeof progress`, &kind))

	if kind != "object" {
		p.t.Fatalf("the progress counter is not reachable from the page (typeof is %q); "+
			"this helper cannot tell a loading page from a stuck one", kind)
	}

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		var idle bool

		p.run("check for requests in flight",
			chromedp.Evaluate(`progress.inFlight === 0`, &idle))

		// The fade is allowed to outlive the last request by its own length, so the
		// strip is given until it has gone rather than checked the instant the
		// counter reaches zero.
		if idle && !p.visible("#progress") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	var inFlight int

	p.run("read the counter", chromedp.Evaluate(`progress.inFlight`, &inFlight))

	p.t.Fatalf("the page never came to rest: %d request(s) in flight, strip visible: %v"+
		"\n\napplication log:\n%s", inFlight, p.visible("#progress"), p.app.Log())
}

// count is how many elements match, which text cannot answer.
//
// For the things the interface builds per row: a checkbox column derived from the
// rows is right or wrong by the number of checkboxes in it, and reading the table's
// text says nothing about that at all.
func (p *page) count(selector string) int {
	p.t.Helper()

	var out int

	p.run("count "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelectorAll(%q).length`, selector), &out))

	return out
}

// attr reads an attribute as the browser currently has it, which is not always
// what the markup said: the reveal button changes an input's type in place.
func (p *page) attr(selector, name string) string {
	p.t.Helper()

	var out string

	p.run(fmt.Sprintf("read %s of %s", name, selector), chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.getAttribute(%q) ?? ""`, selector, name), &out))

	return strings.TrimSpace(out)
}

// locked reports whether a field refuses typing.
//
// Asked as a property, because the attribute cannot answer it. readonly and disabled
// are boolean attributes: present means true and their value is the empty string, so
// getAttribute returns "" whether the field is locked or wide open. The first version
// of this check compared that string against "" and therefore reported every field as
// editable - including one the interface had correctly locked, which is the expensive
// kind of wrong, because it sends somebody looking for a bug that is not there.
//
// Both mechanisms count. The question is whether anybody can type in the field; which
// of the two achieves that is the interface's business.
func (p *page) locked(selector string) bool {
	p.t.Helper()

	var out bool

	p.run("check whether "+selector+" refuses typing", chromedp.Evaluate(fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			return Boolean(el && (el.readOnly || el.disabled));
		})()`, selector), &out))

	return out
}

// value reads what a form field holds, which text cannot: an input's content is
// its value, and textContent sees nothing there.
func (p *page) value(selector string) string {
	p.t.Helper()

	var out string

	p.run("read the value of "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.value ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}

// consoleErrors returns anything the page logged as an error. A page that
// throws on load still renders, so this is the only way to notice.
func (p *page) jsBroken() bool {
	p.t.Helper()

	var ok bool

	// If app.js failed to parse or threw during init, the functions it defines
	// are not there.
	p.run("check that the script initialised",
		chromedp.Evaluate(`typeof window.gtrTheme === 'object'`, &ok))

	return !ok
}

// ---------------------------------------------------------------- the tests

// The regression this package exists for: signing in has to remove the
// overlay. It authenticated correctly and stayed on screen once.
func TestSigningInDismissesTheLoginScreen(t *testing.T) {
	p := open(t)

	if !p.visible("#login-screen") {
		t.Fatal("the sign-in screen should be showing before anyone signs in")
	}

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// And what is behind it has to be usable, not merely uncovered.
	p.run("wait for the application", chromedp.WaitVisible("#tabs", chromedp.ByID))

	if !p.visible("#tabs") {
		t.Error("the navigation should be visible after signing in")
	}
}

// A wrong password must say so and leave the form usable, rather than clearing
// the screen or hanging.
func TestAFailedSignInKeepsTheFormUsable(t *testing.T) {
	p := open(t)

	p.signIn(harness.AdminEmail, "not-the-password")

	p.run("wait for the error", chromedp.WaitVisible("#login-error", chromedp.ByID))

	if message := p.text("#login-error"); message == "" {
		t.Error("a failed sign-in should say something")
	}

	if !p.visible("#form-login") {
		t.Error("the form must stay usable after a failed attempt")
	}
}

// The stylesheet has a global `[hidden] { display: none !important }` because
// layout rules on label and .login-screen otherwise beat the browser's own.
// Losing it makes hidden elements render, which is how the sign-in overlay got
// stuck.
func TestTheHiddenAttributeIsHonoured(t *testing.T) {
	p := open(t)

	var rendered bool

	p.run("check a hidden element", chromedp.Evaluate(`
		(() => {
			const probe = document.createElement('label');
			probe.hidden = true;
			probe.textContent = 'probe';
			document.body.appendChild(probe);
			const shown = getComputedStyle(probe).display !== 'none';
			probe.remove();
			return shown;
		})()`, &rendered))

	if rendered {
		t.Error("a hidden element renders; the global [hidden] rule is not winning")
	}
}

// The whole interface is one script; if it throws on load, the page still
// renders and nothing works.
func TestTheScriptInitialisesWithoutThrowing(t *testing.T) {
	p := open(t)

	if p.jsBroken() {
		t.Fatalf("app.js did not initialise\n\napplication log:\n%s", p.app.Log())
	}
}

// The setup wizard is shown on a first sign-in and has to be operable: it is
// the first thing an administrator meets.
func TestTheSetupWizardAppearsAndAdvances(t *testing.T) {
	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	p.run("wait for the wizard", chromedp.WaitVisible("#setup-wizard", chromedp.ByID))

	if title := p.text("#setup-step-title"); title == "" {
		t.Error("the wizard should be showing a step")
	}

	// The database comes first, and it offers a way to settle it.
	if !p.visible("#setup-steps") {
		t.Error("the step trail should be visible")
	}

	progress := p.text("#setup-progress")
	if progress == "" {
		t.Error("the wizard should say where in it you are")
	}
}

// Switching tabs has to switch what is on screen. A tab that highlights but
// changes nothing is a broken application with a healthy API.
//
// The built-in administrator's own tabs, which are the four it has: it does not record
// time, so the calendar, the entries and the projects are not on its screen at all.
// Deliberately still the plain sign-in rather than readyWorker - what this checks is
// that clicking a tab shows its panel, and it should stay the cheapest case in the
// suite rather than growing a password change and a second account.
func TestTabsSwitchTheVisiblePanel(t *testing.T) {
	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// Out of the way: it is an overlay, so nothing behind it can be clicked.
	p.settleWizard()

	for _, view := range []struct{ tab, panel string }{
		{`.tab[data-view="roles"]`, "#view-roles"},
		{`.tab[data-view="admin"]`, "#view-admin"},
		{`.tab[data-view="settings"]`, "#view-settings"},
		{`.tab[data-view="users"]`, "#view-users"},
	} {
		p.run("switch to "+view.panel, chromedp.Click(view.tab, chromedp.ByQuery))

		time.Sleep(150 * time.Millisecond)

		if !p.visible(view.panel) {
			t.Errorf("%s should be visible after clicking its tab", view.panel)
		}
	}
}

// The picker writes to localStorage and stamps the document; both have to
// happen or the choice is lost on the next page load.
func TestTheAppearancePickerChangesTheTheme(t *testing.T) {
	p := open(t)

	for _, want := range []string{"dark", "light"} {
		p.run("choose "+want, chromedp.SetValue("#theme-picker", want, chromedp.ByID),
			chromedp.Evaluate(
				`document.querySelector('#theme-picker').dispatchEvent(new Event('change'))`, nil))

		time.Sleep(150 * time.Millisecond)

		var theme string

		p.run("read the theme",
			chromedp.Evaluate(`document.documentElement.dataset.theme`, &theme))

		if theme != want {
			t.Errorf("expected the %s theme, got %q", want, theme)
		}
	}
}

// English is the source language and German is a dictionary over it. If the
// dictionary is not applied, the page stays English - which looks fine and is
// wrong.
func TestSwitchingLanguageTranslatesThePage(t *testing.T) {
	p := open(t)

	// The starting language is whatever the browser asks for - that is the
	// point of the auto-detection - so it is set explicitly rather than
	// assumed. A German Windows made this test fail by being right.
	p.run("start from English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	time.Sleep(150 * time.Millisecond)

	english := p.text(`label[data-i18n="login.email"]`)
	if english == "" {
		t.Fatal("the sign-in form should have a labelled email field")
	}

	p.run("switch to German", chromedp.Evaluate(`applyLanguage('de')`, nil))
	time.Sleep(150 * time.Millisecond)

	german := p.text(`label[data-i18n="login.email"]`)
	if german == english {
		t.Errorf("the label did not change when switching language (still %q)", english)
	}

	// Back to English restores the markup's own text, which is what an
	// untranslated key falls back to.
	p.run("switch back to English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	time.Sleep(150 * time.Millisecond)

	if back := p.text(`label[data-i18n="login.email"]`); back != english {
		t.Errorf("expected %q back, got %q", english, back)
	}
}

// The end-to-end path a person actually walks: sign in, change the password,
// book time, see it in the table.
func TestBookingTimeThroughTheInterface(t *testing.T) {
	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// The server refuses everything until the initial password is replaced, so
	// this is not optional decoration - it is the only way in.
	p.settleWizard()

	p.run("open My account",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-password", chromedp.ByID),
		chromedp.SendKeys(`#form-password input[name="currentPassword"]`,
			harness.AdminPassword, chromedp.ByQuery),
		chromedp.SendKeys(`#form-password input[name="newPassword"]`,
			"a-much-better-password", chromedp.ByQuery),
		p.click(`#form-password button[type="submit"]`),
	)

	// The session survives the change now: the server ends the other devices and
	// keeps this one, because this is the device that just proved it knew the old
	// password. Waiting for a sign-in screen here would wait for ever.
	time.Sleep(600 * time.Millisecond)

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
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "Booked in a browser") {
			return
		}

		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("the booking never appeared in the table\n\ntable:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}

// Clicking a time entry in the calendar has to open it for correcting.
//
// The calendar could be read but not acted on: the day opened a table of
// entries, and there it stopped. Worse, the interface had no way to change an
// entry at all - the API has taken a full update from the beginning, but the only
// route the screen offered was deleting the entry and typing it again. So this
// covers both halves: the row opens the entry, and saving it corrects that entry
// instead of booking a second one.
func TestACalendarEntryOpensForEditing(t *testing.T) {
	p := open(t)
	p.readyWorker()

	// On today, so it lands in the month the calendar opens on - the date field
	// already points there.
	p.run("book an entry",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "3.5", chromedp.ByQuery),
		chromedp.SendKeys(`#form-timesheet input[name="description"]`,
			"Typed with a slip", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	p.run("open today in the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible(".cal-day.has-entries", chromedp.ByQuery),
		p.click(".cal-day.has-entries"),
		chromedp.WaitVisible("#table-calendar-day tbody tr.clickable", chromedp.ByQuery),
	)

	// A cell rather than the row, so the click cannot land on one of the action
	// buttons the row also carries.
	p.run("click the entry", p.click("#table-calendar-day tbody tr.clickable td"))

	time.Sleep(300 * time.Millisecond)

	if !p.visible("#view-timesheets") {
		t.Fatalf("clicking an entry should bring up the form that edits it\n\napplication log:\n%s",
			p.app.Log())
	}

	if id := p.value(`#form-timesheet input[name="id"]`); id == "" {
		t.Error("the form is not pointed at the entry that was clicked")
	}

	if got := p.value(`#form-timesheet input[name="durationHours"]`); got != "3.5" {
		t.Errorf("the form holds %q hours, want the entry's 3.5", got)
	}

	if got := p.value(`#form-timesheet input[name="description"]`); got != "Typed with a slip" {
		t.Errorf("the form holds the description %q, want the entry's", got)
	}

	// Saving the correction has to change that entry, not add another one.
	p.run("correct the hours",
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "4.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "4.25") {
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	var rows int

	p.run("count the entries", chromedp.Evaluate(
		`document.querySelectorAll('#table-timesheets tbody tr').length`, &rows))

	if rows != 1 {
		t.Errorf("there are %d entries after correcting one; the save booked a second entry",
			rows)
	}

	if body := p.text("#table-timesheets tbody"); !strings.Contains(body, "4.25") {
		t.Errorf("the corrected hours never reached the table\n\ntable:\n%s\n\napplication log:\n%s",
			body, p.app.Log())
	}

	// And the form goes back to booking, or the next entry typed would silently
	// overwrite the one just corrected.
	if id := p.value(`#form-timesheet input[name="id"]`); id != "" {
		t.Errorf("the form is still pointed at entry %s after saving", id)
	}
}
