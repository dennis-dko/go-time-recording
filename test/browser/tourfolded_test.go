//go:build browser

package browser

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The walk through opens what it is about to point at.
//
// The bar folds on a screen too narrow to hold it: the navigation lives behind
// the burger there, and so do the appearance picker, the language and the way
// out. A step whose target is inside that fold has nothing to ring -
// getBoundingClientRect answers all zeros for an element with no box - so the
// spotlight becomes a twelve-pixel square in the top left corner of the screen
// and the bubble beside it explains something nobody can see.
//
// Measured on a 390px screen before this was fixed: the second step,
// "Everything lives up here", ringed 12x12 at (-6,-6) while the navigation it
// describes was folded away behind the burger.
//
// The claim is about every step rather than about that one, because what the
// tour has to know is that the bar folds - not which of its controls happen to
// be inside the fold today.
func TestNoTourStepPointsAtSomethingFoldedAway(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	// readyAdmin has already recorded the walk as seen, so it is asked for here
	// rather than waited for.
	p.run("start the tour", chromedp.Evaluate(`startTour()`, nil))
	p.run("wait for it", chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	var total int

	p.evalJSON(`JSON.stringify(tour.steps.length)`, &total)

	// However many steps an administrator is offered. What this needs is a walk
	// that reaches the bar's own controls, which are its first and its last.
	if total < 5 {
		t.Fatalf("the walk came to %d steps, so it is not the one this is about", total)
	}

	for step := 1; step <= total; step++ {
		ring := p.tourRing()

		switch {
		case !ring.Shown:
			t.Errorf("step %d of %d, %q, points at %s, which has no box on the screen",
				step, total, ring.Title, ring.Target)
		case !ring.Around:
			t.Errorf("step %d of %d, %q, rings %s while %s is %s",
				step, total, ring.Title, fmtBox(ring.Ring), ring.Target,
				fmtBox(ring.Where))
		}

		if step == total {
			break
		}

		p.run("next step", p.click("#tour-next"))
		p.waitChanged("#tour-title", ring.Title)
	}
}

// box is a rectangle read out of the page.
type box struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// tourRingReading is where the spotlight is, and what the step it belongs to is
// about.
type tourRingReading struct {
	Title   string  `json:"title"`
	Target  string  `json:"target"`
	Shown   bool    `json:"shown"`
	Around  bool    `json:"around"`
	Ring    box     `json:"ring"`
	Where   box     `json:"where"`
	ScrollY float64 `json:"scrollY"`
}

// tourRing answers where the spotlight is once the step has stopped moving.
//
// Two things move after a step is rendered, and both of them have caught this
// case out. placeTour measures two animation frames later - the delay is what
// lets a view that has just been switched to settle - so a reading taken the
// moment the bubble's title changed is the previous step's ring. And the step
// scrolls its target to the middle of the screen with behavior: 'smooth', which
// is a few hundred milliseconds of everything on the page moving: a press on
// Next during it computes the button's position, then lands where the button no
// longer is, and the walk sits on the same step until the case gives up.
//
// So this waits for the page to be at rest - two identical readings, the scroll
// position included - rather than for a guessed length of time. It gives up
// after a few seconds so that a step which never settles is reported rather
// than waited out.
func (p *page) tourRing() tourRingReading {
	p.t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	var last tourRingReading

	for {
		now := p.readTourRing()

		if now == last || time.Now().After(deadline) {
			return now
		}

		last = now

		time.Sleep(80 * time.Millisecond)
	}
}

// readTourRing takes one reading of the spotlight and the step it belongs to.
func (p *page) readTourRing() tourRingReading {
	p.t.Helper()

	var reading tourRingReading

	p.evalJSON(`JSON.stringify((() => {
		const step = tour.steps[tour.index];
		const target = document.querySelector(step.target);
		const spot = document.querySelector('#tour-spotlight').getBoundingClientRect();
		const seen = target ? target.getBoundingClientRect() : new DOMRect();

		// The room placeTour leaves around what it rings.
		const pad = 6;
		const near = (a, b) => Math.abs(a - b) <= 2;

		return {
			title: document.querySelector('#tour-title').textContent,
			target: step.target,
			shown: seen.width > 0 && seen.height > 0,
			around: near(spot.left + pad, seen.left) && near(spot.top + pad, seen.top)
				&& near(spot.width - pad * 2, seen.width)
				&& near(spot.height - pad * 2, seen.height),
			ring: { left: spot.left, top: spot.top, width: spot.width, height: spot.height },
			where: { left: seen.left, top: seen.top, width: seen.width, height: seen.height },
			scrollY: window.scrollY,
		};
	})())`, &reading)

	return reading
}

// fmtBox writes a rectangle the way a failure wants to read it.
func fmtBox(b box) string {
	return fmt.Sprintf("%.0fx%.0f at %.0f,%.0f", b.Width, b.Height, b.Left, b.Top)
}
