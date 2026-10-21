//go:build browser

package browser

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// What a finger presses on a telephone is at least 24 pixels square.
//
// The boxes that select rows for deleting were their own 13 pixels and nothing
// more, the box that selects them all the same, and the mark in the footer that
// leads to the source 18 - targets a finger misses as often as it hits, beside
// the next row's box. The boxes keep their size and gain a label around them,
// which passes a tap on to its box; the mark gains room drawn by nothing, so
// neither it nor its box moves. Decided on 2026-10-17.
//
// Asked of the page rather than of a box's size: every corner of a 24-pixel
// square around the middle of each target has to land on that target - the
// control itself, something inside it, or the label that passes taps to it.
func TestWhatAFingerPressesIsBigEnough(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "sven@example.com", "a-long-enough-password", "user")

	p.run("a telephone", chromedp.EmulateViewport(390, 844))
	p.run("open the accounts", chromedp.Evaluate(`switchView('users')`, nil),
		chromedp.Evaluate(`refreshAll()`, nil, awaitPromise))
	p.waitShown("#table-users tbody .row-pick")

	var missed []string

	p.evalJSON(`JSON.stringify(['#table-users thead .pick-all', '#table-users tbody .row-pick', '#footer-source'].flatMap((selector) => {
		const element = document.querySelector(selector);
		element.scrollIntoView({ block: 'center', inline: 'center' });

		const box = element.getBoundingClientRect();
		const x = box.left + box.width / 2;
		const y = box.top + box.height / 2;
		const lands = (hit) => hit === element || element.contains(hit) || hit?.closest('label')?.control === element;

		return [[-11.5, -11.5], [11.5, -11.5], [-11.5, 11.5], [11.5, 11.5]]
			.filter(([dx, dy]) => !lands(document.elementFromPoint(x + dx, y + dy)))
			.map(([dx, dy]) => selector + ' at ' + dx + ',' + dy);
	}))`, &missed)

	for _, miss := range missed {
		t.Errorf("on a telephone a finger 24 pixels across misses %s from its middle", miss)
	}

	// And the room around a box is really pressable: a tap just inside the corner
	// of what is measured above ticks it.
	var corner struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const box = document.querySelector('#table-users tbody .row-pick');
		box.scrollIntoView({ block: 'center', inline: 'center' });
		const target = (box.closest('label') ?? box).getBoundingClientRect();

		return { x: target.left + 2, y: target.top + 2 };
	})())`, &corner)

	p.run(fmt.Sprintf("tap at %.0f,%.0f", corner.X, corner.Y), chromedp.MouseClickXY(corner.X, corner.Y))

	var ticked bool

	p.run("read the box", chromedp.Evaluate(`document.querySelector('#table-users tbody .row-pick').checked`, &ticked))

	if !ticked {
		t.Error("a tap inside the room around a row's box did not tick it")
	}
}

// Everything pressed on a touch screen is big enough to press, however wide the
// screen is.
//
// The rule that gives a finger 44 pixels applied below 900 pixels of width, on
// the reasoning that a small screen is a touch screen. What it missed is the
// other way round: an iPad held sideways is 1,024 to 1,366 pixels wide and still
// pressed with a finger, and there the tabs were 37 pixels tall, the appearance
// and language pickers and the way out 40. Asked of a touch screen, which is
// what the stylesheet can ask about too.
func TestEveryControlIsBigEnoughToPressOnATabletHeldSideways(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	for _, size := range []struct{ w, h int64 }{{1024, 768}, {1180, 820}, {1366, 1024}} {
		p.run(fmt.Sprintf("a tablet, %dx%d", size.w, size.h),
			chromedp.EmulateViewport(size.w, size.h, chromedp.EmulateMobile, chromedp.EmulateTouch))

		var coarse bool

		p.run("is it a touch screen", chromedp.Evaluate(`matchMedia('(pointer: coarse)').matches`, &coarse))

		if !coarse {
			t.Fatalf("at %dx%d the stylesheet is not told it is a touch screen, so this proves nothing",
				size.w, size.h)
		}

		for _, view := range []string{"welcome", "users", "roles", "settings", "admin"} {
			p.run("open "+view, chromedp.Evaluate(fmt.Sprintf("switchView(%q)", view), nil))

			if small := p.tooSmallToPress(); len(small) > 0 {
				t.Errorf("at %dx%d on the %s screen these are too small to press with a finger: %v",
					size.w, size.h, view, small)
			}
		}
	}
}
