//go:build browser

package browser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// telephone is a screen a telephone offers, and which way up it is held.
type telephone struct {
	name          string
	width, height int64
	onItsSide     bool
}

// emulate makes the browser that telephone: its size, its touch screen, the
// way it is held, and a viewport that honours the page's own meta tag.
func (s telephone) emulate() chromedp.Action {
	way := chromedp.EmulatePortrait
	if s.onItsSide {
		way = chromedp.EmulateLandscape
	}

	return chromedp.EmulateViewport(s.width, s.height,
		chromedp.EmulateMobile, chromedp.EmulateTouch, way)
}

// telephones are the screens the walk through has to fit. The smallest both
// ways round, because each is the worst case for one dimension: upright is the
// narrowest a bubble gets, and on its side the shortest.
var telephones = []telephone{
	{"a small telephone upright", 320, 568, false},
	{"a telephone upright", 390, 844, false},
	{"a telephone on its side", 844, 390, true},
	{"a small telephone on its side", 568, 320, true},
}

// Every step of the walk through is wholly on the screen of a telephone, held
// either way round, in either language.
//
// placeTour bounded the bubble against the right edge and against nothing else.
// It put the bubble below the target where there was room and above it
// otherwise - and "above" never asked whether there was room there either. On a
// telephone on its side a card is taller than the screen, so there is room on
// neither side, and the bubble went above the top of the screen with the step's
// words and its buttons in it.
//
// The two smallest screens are walked again in German, because its sentences
// are the longer ones: a step that fits in English is the one that can need
// scrolling in German. The language is chosen at a desktop's size, since on a
// telephone the picker is folded away behind the burger.
func TestEveryTourStepIsWhollyOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	for _, screen := range telephones {
		p.walkTheTourOn(t, screen, screen.name)
	}

	p.run("back to a desktop", chromedp.EmulateReset())
	p.chooseLanguage("de")

	for _, screen := range []telephone{telephones[0], telephones[3]} {
		p.walkTheTourOn(t, screen, screen.name+", in German")
	}
}

// walkTheTourOn walks the whole tour on one screen and reports every step that
// is not wholly readable on it.
//
// The walk is advanced from script rather than by pressing Next, because Next is
// inside the bubble: when the bubble is off the screen, a press cannot reach it,
// and the case would report a walk that stalled instead of the step that put it
// there.
func (p *page) walkTheTourOn(t *testing.T, screen telephone, label string) {
	t.Helper()

	p.run(label, screen.emulate())
	p.run("start the tour", chromedp.Evaluate(`startTour()`, nil))
	p.run("wait for it", chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	var total int

	p.evalJSON(`JSON.stringify(tour.steps.length)`, &total)

	for step := 1; step <= total; step++ {
		reading := p.tourRing()

		if problems := p.stepProblems(reading); len(problems) > 0 {
			t.Errorf("%s, step %d of %d, %q: %s", label, step, total,
				reading.Title, strings.Join(problems, "; "))
		}

		if step == total {
			break
		}

		p.run("next step", chromedp.Evaluate(`document.querySelector('#tour-next').click()`, nil))
		p.waitChanged("#tour-title", reading.Title)
	}

	p.run("put the tour away", chromedp.Evaluate(`putTheTourAway()`, nil))
}

// The bubble follows the telephone when it is turned while a step is showing.
//
// Turning a telephone is a resize, and a resize has to move the bubble as well as
// the ring: a step placed for an upright screen and left where it was is off the
// bottom of the same screen on its side.
func TestTurningTheTelephoneKeepsTheStepOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	upright, onItsSide := telephones[1], telephones[2]

	p.run(upright.name, upright.emulate())
	p.run("start the tour", chromedp.Evaluate(`startTour()`, nil))
	p.run("wait for it", chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	var total int

	p.evalJSON(`JSON.stringify(tour.steps.length)`, &total)

	// Half way through, where the steps point into the views rather than at the
	// bar that is at the top of every one of them.
	for step := 1; step < total/2; step++ {
		was := p.tourRing().Title

		p.run("next step", chromedp.Evaluate(`document.querySelector('#tour-next').click()`, nil))
		p.waitChanged("#tour-title", was)
	}

	p.run(onItsSide.name, onItsSide.emulate())

	reading := p.tourRing()

	if problems := p.stepProblems(reading); len(problems) > 0 {
		t.Errorf("turned onto its side at %q: %s", reading.Title, strings.Join(problems, "; "))
	}
}

// stepProblems says every way a step is not wholly readable on the screen, in
// words a failure can print.
//
// Half a pixel of slack, because a rectangle read out of a browser is fractional
// and one that ends at 390.25 on a 390-pixel screen is on it.
func (p *page) stepProblems(r tourRingReading) []string {
	p.t.Helper()

	const slack = 0.5

	b := r.Bubble

	var problems []string

	if b.Top < -slack {
		problems = append(problems, fmt.Sprintf("the bubble starts %.0fpx above the top of a %.0fx%.0f screen",
			-b.Top, r.ViewWidth, r.ViewHeight))
	}

	if bottom := b.Top + b.Height; bottom > r.ViewHeight+slack {
		problems = append(problems, fmt.Sprintf("the bubble ends %.0fpx below the bottom of a %.0fx%.0f screen",
			bottom-r.ViewHeight, r.ViewWidth, r.ViewHeight))
	}

	if b.Left < -slack {
		problems = append(problems, fmt.Sprintf("the bubble starts %.0fpx left of a %.0fx%.0f screen",
			-b.Left, r.ViewWidth, r.ViewHeight))
	}

	if right := b.Left + b.Width; right > r.ViewWidth+slack {
		problems = append(problems, fmt.Sprintf("the bubble overhangs the right edge of a %.0fx%.0f screen by %.0fpx",
			r.ViewWidth, r.ViewHeight, right-r.ViewWidth))
	}

	if r.Crammed {
		problems = append(problems, "the step's words do not fit inside the bubble and have to be scrolled to")
	}

	// A page wider than the screen is not a scrollbar on a telephone but a zoom:
	// the browser shrinks everything until it fits, and the screen it reports
	// grows to match - so a bubble can be inside that screen and still be too
	// small to read. Measured on the unfixed walk: the ring around the accounts
	// table made the page 771 pixels wide on every telephone upright.
	var wider float64

	p.evalJSON(`JSON.stringify(document.documentElement.scrollWidth - document.documentElement.clientWidth)`, &wider)

	if wider > slack {
		problems = append(problems, fmt.Sprintf("the page is %.0fpx wider than the screen while this step shows, "+
			"so a telephone zooms it out", wider))
	}

	return problems
}
