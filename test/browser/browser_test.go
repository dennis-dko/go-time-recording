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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// interactionTimeout bounds one test. Generous, because a cold browser start
// and the application's first migration both land inside it - and because the
// suite now runs several cases at once, where all of that happens on a machine
// doing several times the work.
//
// The case that measures a stopwatch spends forty of these seconds deliberately
// asleep, waiting out the smallest bookable duration; on a busy two-core runner
// the rest of its work has to fit in what is left.
const interactionTimeout = 150 * time.Second

// waitPatience is how long the helpers below wait for something that is on its
// way.
//
// Generous on purpose. These were written against a quiet machine running one
// case at a time; the suite now runs several at once, where the same work takes
// several times as long in wall-clock terms without anything being wrong. A wait
// that expires under that load reports a timeout, which reads like a broken
// feature rather than a busy machine.
//
// It costs nothing when the thing arrives - every one of these returns the
// moment its condition holds - and only lengthens how long a genuine failure
// takes to announce itself. The per-case ceiling above is what bounds that.
const waitPatience = 45 * time.Second

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

	// What the page threw, collected as it happens. Guarded because the listener
	// runs on chromedp's own goroutine.
	mu           sync.Mutex
	scriptErrors []string
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

		// Pinned, because the interface writes figures and dates the way the
		// reader's own browser writes them - so without this the suite asserts
		// against whatever locale the machine running it happens to have. Five
		// cases search the time table for "1.11" and found "1,11" on a German
		// machine: the application was right and the suite was not portable, which
		// is worse, because it trains whoever runs it locally to expect red.
		//
		// CI is en-US and was passing by luck rather than by decision. Stating it
		// here makes that a decision, and the one case that is *about* following
		// the browser compares against that browser's own Intl rather than against
		// a format written down here, so it holds whatever this is set to.
		chromedp.Flag("lang", "en-US"),

		// A window somebody might actually use.
		//
		// chromedp's default is 764px wide, which is narrower than any desktop and
		// wide enough not to be a phone. The interface is responsive, so nothing
		// was broken by it - but the top bar wraps into three rows at that width,
		// and a suite whose geometry is an accident of a default is a suite that
		// disagrees with the machine next to it. The one case that is about narrow
		// screens sets its own size.
		chromedp.WindowSize(1280, 900),

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
	p.watchForScriptErrors()

	p.run("load the interface",
		// So the browser reports what it objects to. Without it a script that
		// cannot be parsed is silent here and shows up as every case timing out.
		cdplog.Enable(),
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#form-login", chromedp.ByID),
	)

	return p
}

// watchForScriptErrors records anything the page throws.
//
// Without this a script that will not parse looks like this: every case in the
// suite fails, each after ninety seconds, saying "load the interface: context
// deadline exceeded" - which describes a page that did not appear and says
// nothing about why. The browser knew exactly why, and threw it away.
//
// A missing semicolon cost two runs of the whole suite before anyone looked at
// the file itself. The engine had the answer at once: SyntaxError, with the line
// number.
//
// Cheap, and it applies to every failure rather than to syntax: an exception in a
// click handler leaves a screen that simply does not respond, which reads exactly
// like a selector that no longer matches.
func (p *page) watchForScriptErrors() {
	chromedp.ListenTarget(p.ctx, func(event any) {
		var recorded string

		switch e := event.(type) {
		case *runtime.EventExceptionThrown:
			// Something that ran and threw: a click handler, a failed await.
			details := e.ExceptionDetails
			recorded = fmt.Sprintf("%s (%s:%d:%d)",
				details.Text, details.URL, details.LineNumber, details.ColumnNumber)

		case *cdplog.EventEntryAdded:
			// Everything the browser itself complains about, and the one that
			// matters most is here rather than above: a script that will not
			// *parse* never runs, so it throws nothing. It is reported as a log
			// entry, which is why listening only for thrown exceptions caught none
			// of it - and a script that does not parse is the failure that takes
			// the whole suite down at once.
			if e.Entry == nil || e.Entry.Level != cdplog.LevelError {
				return
			}

			recorded = fmt.Sprintf("%s (%s:%d)", e.Entry.Text, e.Entry.URL, e.Entry.LineNumber)
		default:
			return
		}

		p.mu.Lock()
		defer p.mu.Unlock()

		p.scriptErrors = append(p.scriptErrors, recorded)
	})
}

// thrown is what the page threw, if anything.
func (p *page) thrown() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.scriptErrors) == 0 {
		return ""
	}

	return "\n\nthe page threw:\n" + strings.Join(p.scriptErrors, "\n")
}

// evalJSON runs an expression that returns JSON and unpacks it.
//
// Walking a map[string]any of chromedp's structured values in Go is far less
// readable than the thing being asserted deserves. The page stringifies, this
// unpacks, and the case reads like a case.
func (p *page) evalJSON(expression string, into any) {
	p.t.Helper()

	var raw string

	p.run("read the page's answer", chromedp.Evaluate(expression, &raw))

	if err := json.Unmarshal([]byte(raw), into); err != nil {
		p.t.Fatalf("reading the page's answer: %v\n\n%.400s", err, raw)
	}
}

// chooseOption picks a value from a select, once that value is there to pick.
//
// A select is in the markup from the start and its options arrive with a
// loader, so there is a window in which the element exists and is empty.
// chromedp's SetValue inside that window fails with "could not set value on
// node", which reads as a broken control rather than a case that arrived early -
// and it does so rarely enough to look like a different bug each time.
func (p *page) chooseOption(selector, value string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(waitPatience)

		for {
			var ready bool

			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(
				`Boolean(document.querySelector(%q)?.querySelector('option[value=%q]'))`,
				selector, value), &ready)); err != nil {
				return err
			}

			if ready {
				return chromedp.Run(ctx, chromedp.SetValue(selector, value, chromedp.ByQuery))
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("%s never offered the option %q", selector, value)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// run executes actions and fails the test with the application's log attached,
// which is usually where the reason is.
func (p *page) run(what string, actions ...chromedp.Action) {
	p.t.Helper()

	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v%s\n\napplication log:\n%s",
			what, err, p.thrown(), p.app.Log())
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

	// And then put away again, once nothing else is in flight.
	//
	// The wizard is not only hidden by the line above, it is decided by a
	// loader: signing in asks the server what is still outstanding and shows the
	// wizard if anything is. That question can already be on its way when the
	// two calls above answer it, and its answer - taken before they landed -
	// puts the wizard straight back up.
	//
	// Which is what it did, over the whole page. Every tab click went to the
	// wizard's card instead of the tab, and the case reported whatever was
	// underneath: a tab that does not switch, or a password that would not save.
	// It took a report of what lay over each tab to see it, after two wrong
	// explanations.
	p.atRest()

	p.run("put the wizard away", chromedp.Evaluate(
		`(() => { document.querySelector('#setup-wizard').hidden = true; return 1; })()`, nil))

	p.waitGone("#setup-wizard")
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

	// A first sign-in opens the walk through by itself now - it used to be offered
	// by a modal with a "Not now" beside it, and that button recorded the tour as
	// seen, so the one control that looked like "later" meant "never". Skipping it
	// is what every case but the tour's own wants.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if p.visible("#tour-bubble") {
			p.run("skip the walk through", p.click("#tour-end"))
			p.waitGone("#tour-bubble")

			break
		}

		if p.visible("#view-welcome") {
			break
		}

		time.Sleep(150 * time.Millisecond)
	}

	// And the greeting is a screen rather than something over one, so a sign-in
	// with nothing to go back to lands on it. Carrying on is what a person does
	// next, and it puts the case on the screen it came to look at.
	if p.visible("#view-welcome") {
		p.run("carry on from the greeting", p.click("#welcome-continue"))
		p.waitGone("#view-welcome")
	}

	// Not an error if neither happened: an account that has already been greeted,
	// or one that landed on a remembered screen, is a perfectly ordinary state.
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

// drag takes hold of the middle of an element, moves the pointer and lets go.
//
// Real input rather than a call to the page's own functions: what is being asked
// about here is whether a corner of the selection can be grabbed and pulled, and
// a test that sets the selection directly would pass with nothing wired to the
// corner at all.
//
// Moved in steps, because one jump from press to release is not a drag: the
// handler that follows the pointer only hears about positions it is told about.
func (p *page) drag(selector string, dx, dy float64) {
	p.t.Helper()

	var at struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	p.evalJSON(fmt.Sprintf(`JSON.stringify((() => {
		const r = document.querySelector(%q).getBoundingClientRect();

		return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
	})())`, selector), &at)

	moves := []chromedp.Action{
		chromedp.MouseEvent(input.MousePressed, at.X, at.Y,
			chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	}

	const steps = 6

	for step := 1; step <= steps; step++ {
		share := float64(step) / steps

		moves = append(moves, chromedp.MouseEvent(input.MouseMoved,
			at.X+dx*share, at.Y+dy*share, chromedp.ButtonType(input.Left)))
	}

	moves = append(moves, chromedp.MouseEvent(input.MouseReleased, at.X+dx, at.Y+dy,
		chromedp.ButtonType(input.Left), chromedp.ClickCount(1)))

	p.run("drag "+selector, moves...)
}

// settled waits until every screen has been filled from the server.
//
// Almost every case in this suite reads a screen, and every form and card in
// this application is in index.html - so all of them are on screen from the
// first paint and hold nothing until their request lands. A case that reads in
// between finds an empty table, a card that has not yet been taken away, or a
// field about to be overwritten, and reports it as a broken feature.
//
// That was survivable while the cases ran one at a time on a quiet machine. Run
// beside each other on a busy runner and the window is wide enough to fall into
// regularly, in a different case each time.
func (p *page) settled() {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		var state string

		p.run("is it loaded", chromedp.Evaluate(
			`String(document.documentElement.dataset.loaded ?? '')`, &state))

		if state == "yes" {
			return
		}

		time.Sleep(150 * time.Millisecond)
	}

	p.t.Fatalf("the interface never finished loading; the log says:\n%s", p.app.Log())
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
	p.settled()
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

	// And the walk through, which opens by itself for an account that has never
	// seen it - which a fresh installation's built-in administrator has not.
	//
	// This is what "ready" has to mean. The bubble is a modal, so a case handed a
	// screen with it still up gets its next click swallowed and then waits for an
	// effect that was never going to happen. That is what took the v0.2.0 release
	// out: a connection test whose button was never pressed, reported as a box
	// that said nothing - and the case had already guessed as much in a comment
	// about a missed click.
	//
	// Recorded through the API rather than waited for and clicked away, for the
	// same reason settleWizard completes the setup that way: waiting for a modal
	// that has not opened yet costs its whole patience every time it does not
	// come, and doing that in something sixty-nine cases call doubled the suite -
	// from three and a half minutes to seven and a half. Saying "seen" is
	// instant, and it stops the bubble opening at all rather than racing it.
	p.markTourSeen()
}

// markTourSeen records the walk through as seen, so it never opens.
//
// The same endpoint the "skip" button uses, which is what makes this safe: it is
// what the application itself does, not a state only the tests can reach.
func (p *page) markTourSeen() {
	p.t.Helper()

	var status string

	p.run("record the walk through as seen", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';')
				.map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const res = await fetch('/api/v1/me/tour', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ seen: true }),
			});

			return String(res.status);
		})()`, &status, awaitPromise))

	code, err := strconv.Atoi(status)
	if err != nil || code < 200 || code > 299 {
		p.t.Fatalf("could not record the walk through as seen: %s\n\napplication log:\n%s",
			status, p.app.Log())
	}

	// Already open, if it got there first. Nothing to wait for when it did not:
	// the record above is what stops it opening later.
	if p.visible("#tour-bubble") {
		p.run("skip the walk through", p.click("#tour-end"))
		p.waitGone("#tour-bubble")
	}
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

	p.createOrdinaryAccount(p.t, workerEmail, workerPassword)

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")
	p.settled()
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

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if !p.visible(selector) {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.t.Fatalf("%s is still visible after 20s\n\npage: %s\n\napplication log:\n%s",
		selector, p.state(), p.app.Log())
}

// state describes what the page looked like, for a wait that ran out of time.
//
// "It never appeared" and "it appeared and something took it away again" fail
// the same way and are fixed in completely different places, and the log alone
// separates them only when the server was involved. This says which screen is
// up, what the tabs offer, whether the load finished and what is still in
// flight - enough to tell a click that never landed from one that was undone.
func (p *page) state() string {
	p.t.Helper()

	var out string

	p.run("describe the page", chromedp.Evaluate(`JSON.stringify({
		loaded: document.documentElement.dataset.loaded ?? null,
		shown: [...document.querySelectorAll('.view')].filter(v => !v.hidden).map(v => v.id),
		activeTab: document.querySelector('.tab.active')?.dataset.view ?? null,
		tabs: [...document.querySelectorAll('.tab')].filter(v => !v.hidden).map(v => v.dataset.view),
		dialogs: [...document.querySelectorAll('dialog[open]')].map(v => v.id || v.className),
		hash: location.hash,
		inFlight: typeof progress === 'object' ? progress.inFlight : null,
		notice: document.querySelector('#toast')?.textContent?.trim()?.slice(0, 160) ?? '',
		overTabs: [...document.querySelectorAll('.tab')].filter(t => !t.hidden).map(t => {
			const box = t.getBoundingClientRect();
			const top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
			if (top === t || t.contains(top)) return t.dataset.view + ':ok';

			// The owning element's id, not only the class of whatever pixel was
			// hit: "overlay-card" names a shape, and what matters is which
			// overlay it belongs to.
			const owner = top?.closest?.('[id]');

			return t.dataset.view + ':' + (owner?.id || top?.className || 'nothing');
		}),
	})`, &out))

	return out
}

// waitShown is waitGone's other half: it waits for something to appear.
//
// Written because its absence was being covered by a fixed sleep, and a fixed
// sleep is a guess about how busy the machine is. TestTabsSwitchTheVisiblePanel
// slept 150ms after clicking a tab and then asked whether the panel was up; on a
// loaded runner three of its four tabs were not, and the suite reported a broken
// application over a slow afternoon.
//
// The patience is the same as everywhere else here: long enough that only a real
// failure reaches it, and it costs nothing when the answer arrives at once.
func (p *page) waitShown(selector string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if p.visible(selector) {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	p.t.Fatalf("%s never became visible within %s\n\npage: %s\n\napplication log:\n%s",
		selector, waitPatience, p.state(), p.app.Log())
}

// waitChanged waits until the text at a selector is something other than what it
// was, and answers with the new value.
//
// The shape it replaces is: do something, sleep, read, and complain if the
// reading has not changed. That asserts a duration rather than a change - the
// reading is only stale if the machine was slower than the guess, and on a loaded
// runner the guess is the thing that fails.
//
// Waiting for the change says what the test means, and a change that never comes
// is reported as itself rather than as an unexpected equality.
func (p *page) waitChanged(selector, was string) string {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if now := p.text(selector); now != was {
			return now
		}

		time.Sleep(50 * time.Millisecond)
	}

	p.t.Fatalf("%s still reads %q after %s\n\napplication log:\n%s",
		selector, was, waitPatience, p.app.Log())

	return ""
}

// waitEvaluates waits until a snippet of script answers what is wanted.
//
// The third of these, and the last shape the suite had been sleeping through:
// something that is read out of the document rather than out of an element's
// text. Same reasoning as the other two - the reading is only wrong if the
// machine was slower than the guess.
func (p *page) waitEvaluates(what, js, want string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	var got string

	for time.Now().Before(deadline) {
		p.run("read "+what, chromedp.Evaluate(js, &got))

		if got == want {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	p.t.Fatalf("%s is %q after %s, want %q", what, got, waitPatience, want)
}

func (p *page) text(selector string) string {
	p.t.Helper()

	var out string

	p.run("read "+selector, chromedp.Evaluate(fmt.Sprintf(
		`document.querySelector(%q)?.textContent ?? ""`, selector), &out))

	return strings.TrimSpace(out)
}

// chooseLanguage switches the interface language and waits for what that starts.
//
// Choosing a language saves it to the account, and the save is followed by a
// reload of every screen - which refills every form on its way past. A case that
// switches language and types straight afterwards is racing that reload: on a
// slow run the answer lands after the typing and puts the fields back the way
// the server has them, and the case then fails complaining about a value it set
// itself. That is what "„“ ist keine Datenbank" was, in a case that had
// just chosen postgres.
//
// So the switch is not finished when the labels change; it is finished when
// nothing is in flight any more.
func (p *page) chooseLanguage(code string) {
	p.t.Helper()

	p.run("switch the language to "+code, p.chooseOption("#language-picker", code))
	p.atRest()
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

	deadline := time.Now().Add(waitPatience)

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
// location is the address bar, which the interface writes the current screen
// into - so it is also what a sign-out has to let go of.
func (p *page) location() string {
	p.t.Helper()

	var out string

	p.run("read the address", chromedp.Evaluate(`location.href`, &out))

	return out
}

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	p := open(t)

	if p.jsBroken() {
		t.Fatalf("app.js did not initialise\n\napplication log:\n%s", p.app.Log())
	}
}

// The setup wizard is shown on a first sign-in and has to be operable: it is
// the first thing an administrator meets.
func TestTheSetupWizardAppearsAndAdvances(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// Out of the way: it is an overlay, so nothing behind it can be clicked.
	p.settleWizard()

	// And so is the walk through, which was the actual fault here.
	//
	// It opens by itself on a first sign-in - startTour runs whenever the account
	// has not seen it, and a fresh installation's built-in administrator has not.
	// Its bubble is a modal, so the tab clicks below landed on it instead, and no
	// amount of waiting afterwards helps: the click never reached the tab.
	//
	// Intermittent because it is a race rather than a rule. The tour opens after
	// the /me it waits on, so whether it is up when the first tab is clicked
	// depends on which arrives first - which is why this failed on two unrelated
	// pull requests and passed on the two beside them.
	p.settleWelcome()

	// And the load has to be finished, not merely far enough along to have drawn
	// the tabs. Signing in ends by choosing which screen to open on and putting
	// the reader back where a reload took them from - both of which switch the
	// view. A tab clicked before that lands, and is then switched away from, and
	// the wait below spends forty-five seconds on a panel that was up for a
	// moment. It reported the click as never having worked.
	p.settled()

	for _, view := range []struct{ tab, panel string }{
		{`.tab[data-view="roles"]`, "#view-roles"},
		{`.tab[data-view="admin"]`, "#view-admin"},
		{`.tab[data-view="settings"]`, "#view-settings"},
		{`.tab[data-view="users"]`, "#view-users"},
	} {
		p.run("switch to "+view.panel, chromedp.Click(view.tab, chromedp.ByQuery))

		// Waited for rather than slept through. This is a click and a class
		// change, so it is usually up within a frame - but "usually" is what a
		// fixed 150ms was betting on, and on a loaded runner it lost.
		p.waitShown(view.panel)
	}
}

// The picker writes to localStorage and stamps the document; both have to
// happen or the choice is lost on the next page load.
func TestTheAppearancePickerChangesTheTheme(t *testing.T) {
	t.Parallel()

	p := open(t)

	for _, want := range []string{"dark", "light"} {
		p.run("choose "+want, chromedp.SetValue("#theme-picker", want, chromedp.ByID),
			chromedp.Evaluate(
				`document.querySelector('#theme-picker').dispatchEvent(new Event('change'))`, nil))

		p.waitEvaluates("the theme", `document.documentElement.dataset.theme`, want)
	}
}

// English is the source language and German is a dictionary over it. If the
// dictionary is not applied, the page stays English - which looks fine and is
// wrong.
func TestSwitchingLanguageTranslatesThePage(t *testing.T) {
	t.Parallel()

	p := open(t)

	// The starting language is whatever the browser asks for - that is the
	// point of the auto-detection - so it is set explicitly rather than
	// assumed. A German Windows made this test fail by being right.
	p.run("start from English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	english := p.text(`label[data-i18n="login.email"]`)
	if english == "" {
		t.Fatal("the sign-in form should have a labelled email field")
	}

	p.run("switch to German", chromedp.Evaluate(`applyLanguage('de')`, nil))
	german := p.waitChanged(`label[data-i18n="login.email"]`, english)
	if german == english {
		t.Errorf("the label did not change when switching language (still %q)", english)
	}

	// Back to English restores the markup's own text, which is what an
	// untranslated key falls back to.
	p.run("switch back to English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	if back := p.waitChanged(`label[data-i18n="login.email"]`, german); back != english {
		t.Errorf("expected %q back, got %q", english, back)
	}
}

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

// Clicking a time entry in the calendar has to open it for correcting.
//
// The calendar could be read but not acted on: the day opened a table of
// entries, and there it stopped. Worse, the interface had no way to change an
// entry at all - the API has taken a full update from the beginning, but the only
// route the screen offered was deleting the entry and typing it again. So this
// covers both halves: the row opens the entry, and saving it corrects that entry
// instead of booking a second one.
func TestACalendarEntryOpensForEditing(t *testing.T) {
	t.Parallel()

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

	p.waitShown("#view-timesheets")

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

	deadline := time.Now().Add(waitPatience)
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

// waitSignedIn blocks until the session is one the API will actually accept.
//
// The login screen disappearing says the sign-in answered. It does not say the
// cookies it set have made it back into the browser and are being attached to
// requests, and a test that fires an API call in that gap gets "not
// authenticated" - rarely, on a loaded machine, and reported as whatever the
// call was trying to do rather than as the race it is. Asking the API who is
// signed in is the same question the browser will be asked a moment later.
func (p *page) waitSignedIn(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(waitPatience)

	for {
		var status int

		p.run("ask whether the session is established", chromedp.Evaluate(`
			(async () => {
				const r = await fetch('/api/v1/me', { credentials: 'same-origin' });
				return r.status;
			})()`, &status, awaitPromise))

		if status == 200 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the session never became usable: /api/v1/me answers %d\n\n"+
				"application log:\n%s", status, p.app.Log())
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// The sign-in screen is never laid over a session that is working.
//
// The first load starts before there is a session and fails because there is
// none, and its failure puts the sign-in screen up - which is right, until it
// is not. The form is wired and usable from the first paint, so anybody quick,
// or any machine slow enough for the two to overlap, signs in underneath that
// load. Its failure then arrives after theirs succeeded.
//
// What that left was a page signed in, every screen loaded, sitting on a view -
// with a sign-in form across all of it. Firefox in CI reported it three times
// as "the sign-in screen never went away", which is what it looks like from
// outside and says nothing about a race between two loads.
//
// Asked of the rule rather than of the race. Reproducing the timing means
// holding one response back while another lands, and what is worth holding is
// not that ordering but what it violated: this screen is about whether there is
// a session, so it has no business appearing while there is one.
func TestTheSignInScreenIsNeverPutOverASession(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The same call the failing load makes.
	p.run("let the first load fail late", chromedp.Evaluate(`showLogin()`, nil))

	if p.visible("#login-screen") {
		t.Fatal("the sign-in screen went up over a session that was signed in; " +
			"a load that failed before anybody signed in can still be answering")
	}

	// And the application underneath is untouched: this is about not covering it,
	// not about hiding something and leaving the page half dismantled.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`))
	p.waitShown("#view-admin")

	// And in the gap the first attempt at this missed: a sign-in takes the screen
	// down the moment the password is accepted and fills in the account only
	// afterwards, so between the two there is a page that has been signed into
	// and cannot prove it. Asking "is there an account yet" answered no, and the
	// screen went back up over a sign-in that had already worked.
	var hidden bool

	p.run("a load that fails in the gap", chromedp.Evaluate(`
		(() => {
			const account = me.user;
			me.user = null;

			try {
				showLogin();
			} finally {
				me.user = account;
			}

			return document.querySelector('#login-screen').hidden;
		})()`, &hidden))

	if !hidden {
		t.Error("the sign-in screen went up between the password being accepted " +
			"and the account arriving, which is a page that is signed in and " +
			"cannot say so yet")
	}

	// And signing out still gets there, because that gives the screen back.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID))
	p.waitShown("#login-screen")
}

// What was typed beside the clock is still there after a reload.
//
// Almost everything somebody fills in here is in a form, and a form is what a
// draft is filed under. The timer is not: its project and its description sit
// beside the clock rather than in one, because starting a timer is a button
// rather than a submission - so until it is started, the box is the only copy of
// what somebody has typed, and a reload took it.
//
// Once the timer runs the server holds both and gives them back on its own,
// which is why this is about the window before it is started.
func TestWhatWasTypedBesideTheClockSurvivesAReload(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Somebody who records time: the built-in administrator has no timer.
	p.becomeWorker()

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-description", chromedp.ByID))
	p.settled()

	const doing = "Rechnungslauf vorbereitet"

	p.run("say what is being worked on", chromedp.SendKeys(
		"#timer-description", doing, chromedp.ByID))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settled()

	p.run("open the time view again", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-description", chromedp.ByID))

	if got := p.value("#timer-description"); got != doing {
		t.Errorf("the description beside the clock reads %q after a reload; %q was typed",
			got, doing)
	}
}

// A session the server no longer accepts takes the screen with it.
//
// It used to take nothing. The whole interface stayed standing - every screen
// still drawn, every poller still asking, the previous account's name in the
// corner - and a red notice arrived once per click saying to sign in again, on a
// screen with nowhere to do it. Somebody whose session had timed out could only
// get back in by knowing to reload the page.
//
// Ended here by telling the server rather than by waiting one out: what is being
// checked is what the page does when the cookie stops being worth anything, and
// how it stopped is the server's business.
func TestASessionTheServerNoLongerAcceptsTakesTheScreenWithIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Something typed and unsaved, to see that it goes with the rest: a draft
	// belongs to whoever typed it, and the next person at this browser is not
	// them.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	p.run("type something", chromedp.SendKeys(
		`#form-datasource [name="name"]`, "gtr_live", chromedp.ByQuery))

	// The session ends underneath the page, which the page has no way of knowing.
	p.run("end the session behind its back", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map((c) => c.trim())
				.find((c) => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			await fetch('/api/v1/auth/logout', {
				method: 'POST',
				credentials: 'same-origin',
				headers: { 'X-CSRF-Token': csrf },
			});

			return 1;
		})()`, nil, awaitPromise))

	// The next thing anybody does finds out.
	p.run("carry on working", chromedp.Evaluate(
		`fetch('/api/v1/me', { credentials: 'same-origin' })`, nil))
	p.run("ask the way the page asks", chromedp.Evaluate(
		`loadRoles().catch(() => {})`, nil, awaitPromise))

	p.waitShown("#login-screen")

	if said := p.text("#login-error"); said == "" {
		t.Error("the sign-in screen came back without saying why, which reads as " +
			"having been signed out for no reason")
	}

	// And nothing of the previous account is left standing behind it.
	var left string

	p.run("look behind it", chromedp.Evaluate(`
		JSON.stringify({
			drafts: Object.keys(sessionStorage).filter((k) => k.startsWith('gtr.draft.')).length,
			account: Boolean(me.user),
		})`, &left))

	if !strings.Contains(left, `"drafts":0`) || !strings.Contains(left, `"account":false`) {
		t.Errorf("the ended session left something of itself on the page: %s", left)
	}
}
