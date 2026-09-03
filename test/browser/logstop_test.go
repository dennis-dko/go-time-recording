//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A log viewer that has been refused stops asking.
//
// pollLog catches a refusal and calls stopLogPolling, with the reason written
// beside it: "Retrying would produce one failure per interval for as long as the
// tab stays open."
//
// It cannot stop. stopLogPolling clears logView.timer, and this call is running
// *inside* that timer's own callback - so there is nothing left to clear, and the
// line after the poll reschedules regardless:
//
//	void pollLog().finally(() => { if (logViewerActive()) schedulePoll(); });
//
// On a 401 it happens to work, and only by a side effect: the 401 handling in
// api() puts the sign-in screen up and empties me, so logViewerActive answers
// false. A 403 has no such side effect. The session is fine, the permission is
// not - and me.permissions is never refreshed while a session lasts, because the
// once-a-minute poll discards its answer and exists only to carry the revision
// header. So can('settings:manage') stays true for the rest of the session, the
// screen stays up, and the poll comes round every three seconds for ever.
//
// 403 is the ordinary way to reach that: settings:manage withdrawn while the
// administrator has the log open. The endpoint is not free either - its own
// comment says it reads a mutex-guarded buffer.
//
// Driven by refusing the request in the page rather than by taking a right away
// through the interface, because what is under test is what the viewer does with
// a refusal, not how the refusal came about.
func TestARefusedLogViewerStopsAsking(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Every call to the log endpoint refused, and counted.
	p.run("refuse the log endpoint", chromedp.Evaluate(`(() => {
		window.__logCalls = 0;
		const real = window.fetch;
		window.fetch = (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			if (url.includes('/admin/logs')) {
				window.__logCalls += 1;

				// The first one succeeds. That is the whole point: the refusal has
				// to arrive on a poll the timer fired, not on the immediate one -
				// an immediate poll answers in milliseconds and its stop clears a
				// timer that is still waiting, which works. The timer-fired poll
				// *is* that timer, so there is nothing left to clear.
				if (window.__logCalls === 1) return real(input, init);

				return Promise.resolve(new Response(
					JSON.stringify({ error: { message: 'no', code: 'forbidden' } }),
					{ status: 403, headers: { 'Content-Type': 'application/json' } }));
			}
			return real(input, init);
		};
		return true;
	})()`, nil))

	p.run("open the log", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	// Three default intervals and some room, so a viewer that kept going has had
	// several chances to prove it.
	time.Sleep(12 * time.Second)

	var calls int

	p.run("count the refused requests", chromedp.Evaluate(
		`window.__logCalls`, &calls))

	t.Logf("the log endpoint was asked %d time(s) after being refused", calls)

	if calls > 2 {
		t.Errorf("the log endpoint was asked %d times, so it went on asking after the refusal. stopLogPolling "+
			"runs inside the timer it clears, and the reschedule after it does not "+
			"know a stop happened - so a withdrawn right leaves a request every "+
			"three seconds for as long as the tab is open", calls)
	}
}
