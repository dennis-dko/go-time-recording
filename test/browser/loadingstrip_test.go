//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The loading strip appears while a request is in flight and goes when it is not.
//
// Two failure modes are worth pinning. One is a strip that never appears, which
// makes a slow screen look frozen. The other is a strip that never leaves — an
// in-flight counter that decrements on success only would stick on the first
// failed request and stay for as long as the page is open.
func TestTheLoadingStripAppearsAndGoesAgain(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The screen has just been filled, so the page is given until it is actually at
	// rest. Checking "nothing on screen" the instant readyAdmin returns checked it
	// against a page that was still loading, and passed or failed on how fast the
	// last request happened to be - which is how CI came to report a strip that was
	// showing because a request really was in flight.
	p.atRest()

	// Through api(), which is what counts requests. This used raw fetch, at a path
	// that does not exist - so it never touched the counter and the strip could not
	// have appeared for it. The one time it reported "showing" was the leak below
	// being caught, not the strip working.
	var counted struct {
		Peak    int  `json:"peak"`
		Showing bool `json:"showing"`
	}

	p.run("watch a counted request", chromedp.Evaluate(`(async () => {
		// The log endpoint is the administrator's and always answers, which is all
		// this needs. Through api(), so progressStart and progressDone run.
		const inFlight = api('/admin/logs?limit=1');

		// Read at once: whether the strip is drawn depends on the request outliving
		// a deliberate delay, and a fast one correctly shows nothing. What must be
		// true either way is that the request was counted while it was open.
		const peak = progress.inFlight;

		await new Promise((r) => setTimeout(r, 400));

		const bar = document.querySelector('#progress');
		const showing = Boolean(bar) && !bar.hidden;

		await inFlight.catch(() => {});

		return { peak, showing };
	})()`, &counted, awaitPromise))

	if counted.Peak < 1 {
		t.Error("a request through api() was never counted, so the strip has nothing " +
			"to go on")
	}

	// Not asserted: on a fast local server the request can finish inside the delay,
	// which is the strip behaving correctly. That it is drawn at all for a request
	// that does outlive the delay is pinned deterministically by
	// TestTheStripGoesAwayWhenARequestLandsDuringItsFade.
	t.Logf("the strip was drawn during the request: %v", counted.Showing)

	// What must hold either way: nothing outstanding, nothing on screen.
	p.atRest()

	// And a request that fails leaves it clean too - the counter has to come down in
	// a finally, not on the success path. Through api() again, and api() throws for a
	// 404, which is the point.
	p.run("make a counted request that fails", chromedp.Evaluate(`(async () => {
		try { await api('/does-not-exist'); } catch {}
	})()`, nil, awaitPromise))

	p.atRest()
}

// The strip goes away when a request starts during its fade and finishes quickly.
//
// This is the sequence that left it standing:
//
//	a slow request is shown, finishes, and starts fading out;
//	inside that fade another request starts, which cancels it;
//	that one finishes before it was itself worth showing.
//
// Dropping the pending "show it" was only half the job - nothing re-armed the fade.
// Invisible, because the inner span had already faded, but present, and the counter
// out of step with the screen until some later request happened to put the strip up
// again.
//
// Driven through progressStart and progressDone rather than real requests, because
// the race needs the second request to land inside the fade, and a test that waits
// for that by luck is a test that passes for the wrong reason. No sign-in: these two
// functions touch nothing but #progress and their own counter, and the less that is
// running, the less can be in flight when the replay starts.
//
// This also pins the half its sibling only reports on - that a request outliving the
// delay is drawn at all.
func TestTheStripGoesAwayWhenARequestLandsDuringItsFade(t *testing.T) {
	t.Parallel()

	p := open(t)

	// The counter has to start at zero, or progressStart sees a request already in
	// flight and takes an early return - and then the replay drives nothing while the
	// other request's own fade hides the strip, which is a pass for entirely the wrong
	// reason. The pre-fix code passes this test if that is allowed to happen.
	p.atRest()

	// Two independent facts, kept apart. Collapsed into one boolean, "the strip was
	// never drawn" is reported as "the strip never left", and the reader goes looking
	// for the wrong fault.
	var replay struct {
		Shown  bool `json:"shown"`
		Hidden bool `json:"hidden"`
	}

	// The waits are read out of the application rather than written here, so raising
	// either constant cannot turn this into a failure blaming the other one.
	p.run("replay the sequence", chromedp.Evaluate(`(async () => {
		const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

		// Comfortably past the delay, so the strip is put up rather than skipped.
		progressStart();
		await sleep(PROGRESS_DELAY_MS * 2 + 40);

		const shown = !document.querySelector('#progress').hidden;

		// Finishing starts the fade; a new request lands inside it and finishes
		// before it is itself worth showing.
		progressDone();
		progressStart();
		progressDone();

		// Past the fade, so a re-armed one has certainly run.
		await sleep(PROGRESS_FADE_MS + PROGRESS_DELAY_MS * 2 + 40);

		return { shown, hidden: document.querySelector('#progress').hidden };
	})()`, &replay, awaitPromise))

	if !replay.Shown {
		t.Fatal("the loading strip was never drawn for a request that outlived the " +
			"delay, so the rest of this proves nothing")
	}

	if !replay.Hidden {
		t.Error("the loading strip is still on screen after a request that started " +
			"during its fade and finished quickly; the counter is out of step with what " +
			"is drawn, and stays that way until something puts the strip up again")
	}
}
