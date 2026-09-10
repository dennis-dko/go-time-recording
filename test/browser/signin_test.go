//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

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
	//
	// Waits for the answer rather than for the bubble. This used to stop as soon
	// as the greeting screen appeared, on the reasoning that a tour would have
	// been up by then - and it usually was. Where it was not, the tour opened a
	// moment later over a case that had already moved on, and the page is sealed
	// while it runs: every press afterwards went into the blocker, and the case
	// failed somewhere else entirely, saying something was not visible.
	//
	// dataset.greeted is set when the interface has decided, whichever way it
	// decided, so there is nothing left to guess about.
	// One wait rather than a poll. An attribute selector is a condition the
	// browser can answer for itself, and the suite runs a dozen of these at once
	// - a round trip every hundred milliseconds from each of them is enough
	// traffic to make pages fail to load, which is how the first version of this
	// showed up: unrelated cases dying on net::ERR_ABORTED.
	p.run("wait for the greeting to be decided",
		chromedp.WaitReady("html[data-greeted='yes']", chromedp.ByQuery))

	// Ended rather than clicked away, and asked for whether or not a bubble can
	// be seen at that instant.
	//
	// Clicking was a race with the tour's own first render: data-greeted is set
	// once the walk has started, and a moment later the bubble is laid out and
	// the page is sealed behind it - so a case that looked, saw nothing and
	// carried on had its next press swallowed by the blocker, and failed as
	// something invisible somewhere else. endTour is a no-op where no tour is
	// running, which is the other half of why it can simply be called.
	//
	// The tour's own cases still drive its buttons; this is the door every other
	// case needs to be through.
	p.run("skip the walk through", chromedp.Evaluate(`endTour()`, nil))
	p.waitGone("#tour-bubble")

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
