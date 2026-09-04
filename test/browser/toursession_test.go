//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A session that ends during the walk through does not leave the page sealed.
//
// The tour puts the page out of reach on purpose, and sealThePage explains both
// halves: a fixed blocker that takes every press, because the spotlight carries
// pointer-events: none and everything under the dimming was still clickable; and
// inert on each of the body's children, because a blocker stops the mouse and
// nothing else - Tab still walked into the page behind it.
//
// sealThePage(false) is called from exactly one place, endTour. handBackTheScreen
// does not call it. So when the session ends while the tour is up - a poll
// answered with 401 after a right is withdrawn, or maintenance mode turning on -
// the sign-in screen comes up underneath a walk through belonging to the session
// that just ended: dimmed, inert, describing a step of an account that is gone.
//
// There is a way out, and it is not one a newcomer will find: the tour's own
// bubble is spared from the seal, so Escape or its End button still work. The
// tour is shown to an account on its first sign-in, which is exactly the person
// least placed to work that out.
//
// handBackTheScreen is called directly rather than provoked with a refused
// request. It is the thing under test, and every route to it - the permission
// poll, the log poll, a maintenance 503 - arrives here; going through one of them
// would make the case depend on which poll fires first.
func TestASessionEndingDuringTheTourLeavesThePageUsable(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var started bool

	p.run("start the walk through", chromedp.Evaluate(`(async () => {
		await startTour();

		return tour.active;
	})()`, &started, awaitPromise))

	if !started {
		t.Fatal("the tour did not start, so this case would pass whatever the " +
			"sign-out does")
	}

	// Sealed, which is the state the rest of this is about.
	var sealed bool

	p.run("the page is sealed", chromedp.Evaluate(
		`document.querySelector('#form-login').closest('[inert]') !== null`, &sealed))

	if !sealed {
		t.Fatal("the tour did not seal the page, so there is nothing to be left behind")
	}

	p.run("the session ends underneath it", chromedp.Evaluate(
		`handBackTheScreen('')`, nil))

	var after struct {
		Sealed  bool `json:"sealed"`
		Blocked bool `json:"blocked"`
		Bubble  bool `json:"bubble"`
		Active  bool `json:"active"`
	}

	var out string

	p.run("what is left on the screen", chromedp.Evaluate(`(() => {
		const blocker = document.querySelector('#tour-blocker');
		const bubble = document.querySelector('#tour-bubble');

		return JSON.stringify({
			sealed: document.querySelector('#form-login').closest('[inert]') !== null,
			blocked: blocker ? !blocker.hidden : false,
			bubble: bubble ? !bubble.hidden : false,
			active: tour.active,
		});
	})()`, &out))

	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("reading the screen (%q): %v", out, err)
	}

	if after.Sealed {
		t.Error("the sign-in screen is inert after the session ended: the tour " +
			"sealed the page and only endTour unseals it, so signing in again " +
			"means finding the tour's own way out first")
	}

	if after.Blocked {
		t.Error("the tour's blocker still covers the page, so every press lands on it")
	}

	if after.Bubble {
		t.Error("the tour's bubble still stands over the sign-in screen, describing " +
			"a step of the account that has gone")
	}

	if after.Active {
		t.Error("the tour still counts itself as running after the session it was " +
			"started in ended")
	}
}
