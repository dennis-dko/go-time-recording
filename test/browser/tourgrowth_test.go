//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The ring follows what it marks when that grows under it.
//
// A step is drawn two frames after its screen is switched to, and several of the
// cards the walk points at fill in later than that - the log's lines, the
// telemetry settings. The ring was measured once and drawn again only when the
// window was resized or scrolled, so a card that grew after its step was drawn
// kept a ring around the part of it that had already been there. The walk over
// every step caught it now and then, as a ring 804 pixels high around a form of
// 1,012, and passed on the next run: which of the two arrives first decided it.
//
// So the order is forced here rather than waited for. The step is drawn, and
// then its target grows.
func TestTheTourRingFollowsATargetThatGrows(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("start the tour", chromedp.Evaluate(`startTour()`, nil))
	p.run("wait for it", chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	// On to the first step about a card on a screen: the steps before it point at
	// the bar, which holds nothing that arrives late.
	for {
		var onACard bool

		p.run("ask where the step points",
			chromedp.Evaluate(`tour.steps[tour.index].view !== null`, &onACard))

		if onACard {
			break
		}

		title := p.tourRing().Title

		p.run("next step", p.click("#tour-next"))
		p.waitChanged("#tour-title", title)
	}

	before := p.tourRing()
	if !before.Around {
		t.Fatalf("step %q is not ringed to begin with: the ring is %s and %s is %s",
			before.Title, fmtBox(before.Ring), before.Target, fmtBox(before.Where))
	}

	// What a card does when what it shows arrives.
	p.run("grow the target", chromedp.Evaluate(`(() => {
		const target = document.querySelector(tour.steps[tour.index].target);

		target.style.minHeight = (target.getBoundingClientRect().height + 240) + 'px';
	})()`, nil))

	after := p.tourRing()

	if after.Where == before.Where {
		t.Fatalf("%s did not grow, so this measured nothing: it is still %s",
			after.Target, fmtBox(after.Where))
	}

	if !after.Around {
		t.Errorf("%s grew to %s and the ring stayed at %s",
			after.Target, fmtBox(after.Where), fmtBox(after.Ring))
	}
}
