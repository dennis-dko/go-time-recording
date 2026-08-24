//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Nothing on the page can be used while the tour is running - including the
// thing the tour is pointing at.
//
// The spotlight is one element with an enormous shadow and it carries
// pointer-events: none, so that it does not swallow presses meant for the
// control it rings. The consequence was that every press landed: on the
// highlighted control, and on everything under the dimming as well. A tour
// explaining the stopwatch while somebody starts it is a tour narrating a
// screen that has moved on without it.
func TestTheTourTakesThePageOutOfReachWhileItRuns(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// An ordinary account is walked through on its first sign-in, so the tour is
	// already up - but this asks for it rather than relying on that, because
	// what is being tested is the sealing and not when the walk begins.
	p.run("start the tour", chromedp.Evaluate(`startTour()`, nil))

	p.run("wait for it", chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	onStep := p.text("#tour-title")
	if onStep == "" {
		t.Fatal("the tour is up but has no step, so there is nothing to point at")
	}

	// A tab is the plainest thing on the page and the easiest to prove: pressing
	// one changes the view, and the view is readable from here.
	before := p.currentView()

	// Not p.click: that waits for the element to be clickable and would fail as
	// a timeout rather than as an answer. This dispatches the press wherever the
	// tab is and lets whatever is on top of it receive it.
	p.run("try to press a tab", chromedp.Evaluate(`
		(() => {
			const tab = document.querySelector('.tab[data-view="projects"]');
			if (!tab) return 'no tab';

			const box = tab.getBoundingClientRect();
			const x = box.left + box.width / 2;
			const y = box.top + box.height / 2;

			// What is actually at that point. During the tour it should be the
			// blocker rather than the tab.
			const on = document.elementFromPoint(x, y);

			on?.dispatchEvent(new MouseEvent('click', {
				bubbles: true, clientX: x, clientY: y,
			}));

			return on?.id || on?.className || 'nothing';
		})()`, nil))

	// Given a moment to have gone wrong.
	time.Sleep(400 * time.Millisecond)

	if after := p.currentView(); after != before {
		t.Errorf("pressing a tab during the tour moved from %q to %q", before, after)
	}

	// And the highlighted control itself, which is the half that was asked
	// about. The title is the first step's target and it navigates.
	p.run("try to press the highlighted control", chromedp.Evaluate(`
		(() => {
			const box = document.querySelector('#app-title').getBoundingClientRect();
			const x = box.left + box.width / 2;
			const y = box.top + box.height / 2;

			document.elementFromPoint(x, y)?.dispatchEvent(new MouseEvent('click', {
				bubbles: true, clientX: x, clientY: y,
			}));
		})()`, nil))

	time.Sleep(400 * time.Millisecond)

	if after := p.currentView(); after != before {
		t.Errorf("pressing the highlighted control during the tour moved from %q "+
			"to %q", before, after)
	}

	// The keyboard is the other way in, and a blocker does nothing about it: Tab
	// used to walk into the page behind and Enter pressed what it found.
	var reachable bool

	p.run("is the page still in the tab order", chromedp.Evaluate(`
		(() => {
			const tab = document.querySelector('.tab[data-view="projects"]');
			if (!tab) return false;

			tab.focus();

			return document.activeElement === tab;
		})()`, &reachable))

	if reachable {
		t.Error("the page behind the tour can still be reached with the keyboard")
	}

	// What must still work: the tour's own controls, which are the way out.
	p.run("next step", p.click("#tour-next"))

	if next := p.waitChanged("#tour-title", onStep); next == onStep {
		t.Errorf("the tour's own Next stopped working; still on %q", onStep)
	}

	p.run("leave the tour", p.click("#tour-end"))
	p.waitGone("#tour-bubble")

	// And afterwards the page is a page again.
	p.run("press a tab", p.click(`.tab[data-view="projects"]`))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.currentView() == "projects" {
			return
		}

		time.Sleep(150 * time.Millisecond)
	}

	t.Errorf("the page was left sealed after the tour ended; it shows %q",
		p.currentView())
}

// currentView is which section is on screen.
func (p *page) currentView() string {
	p.t.Helper()

	var view string

	p.run("read the view", chromedp.Evaluate(`
		[...document.querySelectorAll('section.view')]
			.find((section) => !section.hidden)?.id ?? ''`, &view))

	return strings.TrimPrefix(view, "view-")
}
