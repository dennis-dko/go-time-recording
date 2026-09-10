//go:build browser

package browser

import (
	"fmt"
	"math"
	"testing"

	"github.com/chromedp/chromedp"
)

// The navigation has its own line, centred, and nothing lies on top of anything.
//
// Three things that only a browser can answer, and the first preview of this
// layout got two of them wrong: the points overlapped the account beside them,
// and a wrapped row read as centred when it was a block growing from the left.
func TestTheNavigationSitsCentredOnItsOwnLine(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	type box struct {
		Left   float64 `json:"left"`
		Right  float64 `json:"right"`
		Top    float64 `json:"top"`
		Bottom float64 `json:"bottom"`
		Width  float64 `json:"width"`
	}

	read := func() struct {
		Bar  box     `json:"bar"`
		Tabs box     `json:"tabs"`
		Row  box     `json:"row"`
		Gap  float64 `json:"gap"`
		Rows int     `json:"rows"`
	} {
		var out struct {
			Bar  box     `json:"bar"`
			Tabs box     `json:"tabs"`
			Row  box     `json:"row"`
			Gap  float64 `json:"gap"`
			Rows int     `json:"rows"`
		}

		p.evalJSON(`JSON.stringify((() => {
			const rect = (selector) => {
				const r = document.querySelector(selector).getBoundingClientRect();

				return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width };
			};

			const bar = rect('.topbar');
			const row = rect('.topbar-row');
			const tabs = rect('#tabs');

			// How many lines the points are spread over, from their own tops.
			const tops = new Set([...document.querySelectorAll('#tabs .tab')]
				.filter((tab) => !tab.hidden)
				.map((tab) => Math.round(tab.getBoundingClientRect().top)));

			return { bar, row, tabs, gap: tabs.top - row.bottom, rows: tops.size };
		})())`, &out)

		return out
	}

	// Wide enough for the whole row, which is where "centred" has to be exact.
	p.run("a wide window", chromedp.EmulateViewport(1600, 900))

	wide := read()

	if wide.Rows != 1 {
		t.Errorf("the points are spread over %d lines at 1600px", wide.Rows)
	}

	// Centred on the bar, not between neighbours: there are none on this line.
	barMiddle := wide.Bar.Left + wide.Bar.Width/2
	tabsMiddle := wide.Tabs.Left + wide.Tabs.Width/2

	if math.Abs(barMiddle-tabsMiddle) > 2 {
		t.Errorf("the points are centred at %.0f and the bar at %.0f", tabsMiddle, barMiddle)
	}

	// What this application is called sits in the middle, over the navigation: one
	// vertical line down the bar rather than two things anchored to corners. The
	// logo has the left end, and an installation with none leaves that column
	// empty, which takes no room at all.
	var title struct {
		Centre float64 `json:"centre"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const box = document.querySelector('#app-title').getBoundingClientRect();

		return { centre: box.left + box.width / 2 };
	})())`, &title)

	if math.Abs(title.Centre-barMiddle) > 2 {
		t.Errorf("the name is centred at %.0f and the bar at %.0f",
			title.Centre, barMiddle)
	}

	// Its own line: below everything on the row above rather than beside it. This
	// is what the overlap in the first preview looked like from here.
	if wide.Tabs.Top < wide.Row.Bottom {
		t.Errorf("the points start at %.0f, above where the row above ends (%.0f)",
			wide.Tabs.Top, wide.Row.Bottom)
	}

	// The line of space that was asked for.
	if wide.Gap < 28 || wide.Gap > 36 {
		t.Errorf("the space between the two rows is %.0fpx, want about 32", wide.Gap)
	}

	// A long name on the right cannot pull the points off centre, which is the
	// reason this layout was chosen over centring between the neighbours.
	p.run("a very long name", chromedp.Evaluate(`
		(() => {
			document.querySelector('#who').textContent =
				'Maximiliane Freifrau von und zu Beispielhausen · Benutzer & Administrator';

			return 1;
		})()`, nil))

	stretched := read()
	stretchedMiddle := stretched.Tabs.Left + stretched.Tabs.Width/2

	if math.Abs(stretchedMiddle-(stretched.Bar.Left+stretched.Bar.Width/2)) > 2 {
		t.Errorf("a long name moved the points to %.0f", stretchedMiddle)
	}

	// And narrow, where the points are not on this line at all: they fold away
	// behind one control, which TestTheNavigationFoldsAwayOnASmallScreen is about.
	// What is left to check here is the row they used to share.
	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	// Nothing on the narrow screen lies on top of anything else.
	var overlaps string

	p.evalJSON(`JSON.stringify((() => {
		const parts = [...document.querySelectorAll('.topbar-row > *')]
			.filter((part) => !part.hidden)
			.map((part) => ({ name: part.id || part.tagName, box: part.getBoundingClientRect() }));

		const clashes = [];

		for (let i = 0; i < parts.length; i++) {
			for (let j = i + 1; j < parts.length; j++) {
				const a = parts[i].box;
				const b = parts[j].box;

				const over = a.left < b.right - 1 && b.left < a.right - 1
					&& a.top < b.bottom - 1 && b.top < a.bottom - 1;

				if (over) clashes.push(parts[i].name + '/' + parts[j].name);
			}
		}

		return clashes.join(', ');
	})())`, &overlaps)

	if overlaps != "" {
		t.Errorf("these lie on top of each other in the header: %s", overlaps)
	}
}

// Nothing on any screen is wider than the screen.
//
// A page that can be dragged sideways on a telephone is the plainest failure of
// a responsive layout, and this one could be. The nine navigation points were a
// row that did not wrap, scrolling inside itself - except the bar was as wide as
// the row and the window scrolled in its place. Measured at 50px too wide on a
// 390px screen and 80px on a 360px one, on every screen in the application.
//
// What fixed it is that the row is not there below the breakpoint any more; see
// the case below.
func TestNothingIsWiderThanTheScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	for _, width := range []int64{1440, 1024, 820, 600, 480, 390, 360, 320} {
		p.run("resize", chromedp.EmulateViewport(width, 900))

		for _, view := range []string{"welcome", "timesheets", "calendar", "settings"} {
			p.run("open "+view, chromedp.Evaluate(
				fmt.Sprintf("switchView(%q)", view), nil))

			var over float64

			p.evalJSON(`JSON.stringify(
				document.documentElement.scrollWidth - document.documentElement.clientWidth)`,
				&over)

			if over > 1 {
				t.Errorf("at %dpx the %s screen is %.0fpx wider than the window",
					width, view, over)
			}
		}
	}
}

// The navigation folds into one control on a screen too narrow to hold it.
//
// Nine points do not fit beside anything on a telephone. As a row that scrolled
// sideways they showed three and hid six, with nothing on screen to say the
// others were there.
func TestTheNavigationFoldsAwayOnASmallScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Wide: the points are on screen and there is nothing to open.
	p.run("a wide window", chromedp.EmulateViewport(1440, 900))

	if p.visible("#nav-toggle") {
		t.Error("a wide screen offers a way to open a navigation that is already open")
	}

	if !p.visible(`.tab[data-view="settings"]`) {
		t.Error("the points are not on screen at 1440px")
	}

	// Narrow: folded away, behind one control.
	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	if !p.visible("#nav-toggle") {
		t.Fatal("a telephone has no way to reach the navigation")
	}

	if p.visible(`.tab[data-view="settings"]`) {
		t.Error("the points are still on screen on a telephone, where they do not fit")
	}

	// Opening shows all of them, as a list rather than a row that hides six.
	p.run("open the menu", p.click("#nav-toggle"))

	var shown struct {
		Count   int    `json:"count"`
		Rows    int    `json:"rows"`
		Says    string `json:"says"`
		Overrun bool   `json:"overrun"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const tabs = [...document.querySelectorAll('#tabs .tab')].filter((t) => !t.hidden);
		const tops = new Set(tabs.map((t) => Math.round(t.getBoundingClientRect().top)));
		const doc = document.documentElement;

		return {
			count: tabs.length,
			rows: tops.size,
			says: document.querySelector('#nav-toggle').getAttribute('aria-expanded'),
			overrun: doc.scrollWidth > doc.clientWidth + 1,
		};
	})())`, &shown)

	// However many this account may see - an administrator has four, somebody who
	// books time has more. The claim is about how they are laid out, not how many
	// there are.
	if shown.Count < 3 {
		t.Fatalf("the menu holds %d points", shown.Count)
	}

	// One per line: a row that scrolls is what this replaced.
	if shown.Rows != shown.Count {
		t.Errorf("%d points share %d lines, so some of them are side by side",
			shown.Count, shown.Rows)
	}

	if shown.Says != "true" {
		t.Errorf("the control says aria-expanded=%q while the menu is open", shown.Says)
	}

	if shown.Overrun {
		t.Error("opening the menu made the page wider than the window")
	}

	// And it closes on the three things that mean "done": choosing a point,
	// Escape, and a press elsewhere.
	p.run("choose one", p.click(`.tab[data-view="settings"]`))

	if p.attr("#nav-toggle", "aria-expanded") != "false" {
		t.Error("choosing a point left the menu open over the screen it opened")
	}

	p.run("open again", p.click("#nav-toggle"))
	p.run("press escape", chromedp.KeyEvent("\u001b"))

	if p.attr("#nav-toggle", "aria-expanded") != "false" {
		t.Error("Escape did not close the menu")
	}

	p.run("open once more", p.click("#nav-toggle"))
	p.run("press elsewhere", chromedp.Evaluate(
		`document.querySelector('main').click()`, nil))

	if p.attr("#nav-toggle", "aria-expanded") != "false" {
		t.Error("a press outside the menu did not close it")
	}
}

// Everything you press on a telephone is big enough to press.
//
// A small screen is overwhelmingly a touch screen, where the thing being aimed
// with is about a centimetre across rather than a pixel. Measured on a 390px
// screen before this was fixed: the burger 36px tall, the appearance picker 37,
// a tab in the open menu 37, the fields 38, the button that reveals a password
// 26, and the one that dismisses a notice 18.
//
// Forty-four is the size a finger needs, and the number both platform
// guidelines settled on.
func TestEveryControlIsBigEnoughToPressOnAPhone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	// With the navigation open, so its points are measured too.
	p.run("open the menu", p.click("#nav-toggle"))

	for _, view := range []string{"welcome", "users", "roles", "settings"} {
		p.run("open "+view, chromedp.Evaluate(
			fmt.Sprintf("switchView(%q)", view), nil))

		var small []string

		// What a thumb actually lands on, which is not always the element itself.
		// A switch's checkbox is invisible and 22px; the thing being pressed is
		// the label drawn around it. A checkbox in a table is small on purpose;
		// what is pressed is the cell holding it, header cells included - the one
		// that selects every row lives in a th. A file field is drawn by the
		// browser and hardly restylable; its label is what can be given room.
		p.evalJSON(`JSON.stringify(
			[...document.querySelectorAll('button, select, .tab, input:not([type=hidden])')]
				.filter((el) => !el.hidden && el.offsetParent !== null)
				.map((el) => (['checkbox', 'radio', 'file'].includes(el.type)
					? (el.closest('label, td, th') ?? el)
					: el))
				.filter((el) => {
					const box = el.getBoundingClientRect();

					return box.height > 0 && box.height < 44;
				})
				.map((el) => (el.id || el.className || el.tagName) + ':' +
					Math.round(el.getBoundingClientRect().height))
				.filter((v, i, a) => a.indexOf(v) === i)
				.slice(0, 8))`, &small)

		if len(small) > 0 {
			t.Errorf("on the %s screen these are too small to press with a thumb: %v",
				view, small)
		}
	}
}
