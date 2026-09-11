//go:build browser

package browser

import (
	"time"

	"github.com/chromedp/chromedp"
)

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

// settleReleaseWatch takes the release check out of the way before a case says
// what the banner ought to show.
//
// Signing in starts the watch, and the watch fires checkForRelease straight away
// - a round trip to the update feed. Whatever a case then puts on the banner is
// taken down the moment that answer lands, because no newer version exists here
// and showReleaseState hides the banner on "no". Which of the two arrives first
// is a question about how fast the feed answers, so the case passed on this
// machine, where it answers first, and failed on CI, where it answered second:
// "#release-banner never became visible within 45s", 45 seconds spent waiting for
// something the case had already put there and something else had removed.
//
// Three cases say what the banner should show and none of them settled this
// first, which is the shape Section 2 of CLAUDE.md is about - one piece of
// reasoning needed in three places and written down in none. So it is one
// function, and it forces the order rather than hoping for it: let the check that
// is already running finish, stop the watch so no later tick repeats it, and
// silence the feed for the rest of the case so nothing can answer behind its
// back. Pressing "check for updates" is unaffected - that goes to
// /settings/update/check and never through here.
func (p *page) settleReleaseWatch() {
	p.t.Helper()

	p.run("settle the release watch", chromedp.Evaluate(`
		(async () => {
			await checkForRelease();
			stopReleaseWatch();
			checkForRelease = async () => {};

			return 1;
		})()`, nil, awaitPromise))
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
