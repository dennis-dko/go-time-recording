//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Coming back to a tab asks both background questions, not one of them.
//
// The minute beat asks two things together and says why: what this account may
// do, and - "on the same beat and for the same reason" - whether the
// installation is still open to it at all. Both are for the screen somebody is
// reading rather than working in, which is the case neither the revision header
// nor a refused click covers.
//
// The interval skips while the tab is hidden, and the reason that is allowed is
// written above it: coming back "covers however long the tab was hidden in one
// request". The listener that does the covering asks only the first of the two.
//
// So a tab hidden while maintenance was switched on came back to the whole
// interface standing, for up to a minute - which is the state
// checkWhetherStillWelcome exists to prevent: "every card an error and every
// click another one, with nothing saying that was deliberate".
//
// Answered from the page rather than by switching maintenance on for real: this
// is about which questions are asked on the way back, not about the server.
func TestReturningToTheTabAsksWhetherTheInstallationIsStillOpen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("the installation closes while the tab is away", chromedp.Evaluate(`(() => {
		const real = window.fetch;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/api/v1/maintenance')) {
				return new Response(JSON.stringify({ data: {
					enabled: true,
					message: 'Wartung: die Anwendung ist kurz nicht erreichbar.',
				} }), { status: 200, headers: { 'Content-Type': 'application/json' } });
			}

			return real(input, init);
		};

		return true;
	})()`, nil))

	// Looked at again. The listener guards on document.hidden, which is false for
	// a page being driven, so this is the same call it makes on a real return.
	p.run("come back to the tab", chromedp.Evaluate(
		`document.dispatchEvent(new Event('visibilitychange'))`, nil))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#login-screen") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("the tab came back to a closed installation and the interface stayed " +
		"up. The minute beat asks /maintenance alongside /me and says the two go " +
		"together; the listener that covers a hidden tab asks only /me, so the " +
		"gap it is meant to cover is covered by half")
}

// And it asks again whether there is a newer version.
//
// The hourly watch says what it is for: "A day left open should notice a
// release." A day left open is usually a day spent hidden behind other tabs, and
// the interval skips while it is - so the promise rests on the return, exactly as
// the permission check's does. Browsers throttle timers in background tabs
// heavily anyway, which makes the return the reliable moment rather than merely
// the convenient one.
//
// Guarded on the watch running rather than on the permission, so an account that
// is not offered the news does not make the request: startReleaseWatch already
// decides who that is, and this asks the same question by asking whether it
// started.
func TestReturningToTheTabAsksWhetherThereIsANewerVersion(t *testing.T) {
	t.Parallel()

	p := open(t)

	// The administrator, because the release watch runs only for whoever may act
	// on it.
	p.readyAdmin()

	p.run("count what is asked", chromedp.Evaluate(`(() => {
		const real = window.fetch;
		window.__updateAsks = 0;

		window.fetch = (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/api/v1/settings/update')) window.__updateAsks += 1;

			return real(input, init);
		};

		return true;
	})()`, nil))

	p.run("come back to the tab", chromedp.Evaluate(
		`document.dispatchEvent(new Event('visibilitychange'))`, nil))

	deadline := time.Now().Add(waitPatience)

	var asked int

	for time.Now().Before(deadline) {
		p.run("how many times", chromedp.Evaluate(`window.__updateAsks`, &asked))

		if asked > 0 {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("coming back to the tab did not ask whether there is a newer version. " +
		"The watch skips while the tab is hidden and the hourly beat is throttled " +
		"there anyway, so the return is what the promise of noticing a release " +
		"rests on")
}
