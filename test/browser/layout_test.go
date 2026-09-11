//go:build browser

package browser

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/test/harness"
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

	// Every screen the administrator is offered, in both languages. That is the
	// claim above, and it is not what this case checked: it walked four screens as
	// an ordinary account, so the role editor was never measured - and its
	// permission ids ran 15px past a 320px screen on the CI runner's fonts, found
	// only because the guided tour stops there. Read off the tabs rather than
	// listed, so a screen added later is measured without anybody remembering to.
	var views []string

	p.evalJSON(`JSON.stringify([...document.querySelectorAll('.tab[data-view]')]
		.map((tab) => tab.dataset.view))`, &views)

	if len(views) < 6 {
		t.Fatalf("the administrator is offered %d screens; the tabs are being read wrongly", len(views))
	}

	p.everyScreenFits(t, "the administrator", views)
	p.chooseLanguage("de")
	p.everyScreenFits(t, "the administrator, in German", views)

	p.becomeWorker()
	p.everyScreenFits(t, "an ordinary account", []string{"welcome", "timesheets", "calendar", "settings"})
}

// everyScreenFits opens each screen at every width from a desktop down to the
// smallest telephone and reports the ones wider than the window, then goes back
// to a desktop, where the language picker is not folded away.
func (p *page) everyScreenFits(t *testing.T, who string, views []string) {
	t.Helper()

	for _, width := range []int64{1440, 1024, 820, 600, 480, 390, 360, 320} {
		p.run("resize", chromedp.EmulateViewport(width, 900))

		for _, view := range views {
			p.run("open "+view, chromedp.Evaluate(
				fmt.Sprintf("switchView(%q)", view), nil))

			var over float64

			p.evalJSON(`JSON.stringify(
				document.documentElement.scrollWidth - document.documentElement.clientWidth)`,
				&over)

			if over > 1 {
				t.Errorf("as %s, at %dpx the %s screen is %.0fpx wider than the window",
					who, width, view, over)
			}
		}
	}

	p.run("back to a desktop", chromedp.EmulateViewport(1280, 900))
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

// The sign-in screen can be used on a short window.
//
// It is a fixed overlay that centres what it holds, which clips anything too
// tall at both ends - and nothing scrolls it back. The submit button is simply
// not there, on a screen where nothing looks wrong: the form is visible, one
// button short of the bottom.
//
// A laptop with a toolbar is enough to do it once the screen has a notice above
// the card and a mark above that. Which is how this was found: the shipped mark
// started standing in for an absent logo, and a sign-in on a short CI window
// stopped working.
//
// No sign-in needed - open(t) lands here, which is the state this is about.
func TestTheSignInScreenIsUsableOnAShortWindow(t *testing.T) {
	t.Parallel()

	p := open(t)

	// Shorter than the screen's own contents, so this asks about the property
	// rather than about one machine's idea of a window.
	p.run("a short window", chromedp.EmulateViewport(1000, 320),
		chromedp.Sleep(400*time.Millisecond))

	var out struct {
		Overflows    bool    `json:"overflows"`
		Scrolled     float64 `json:"scrolled"`
		ButtonBottom float64 `json:"buttonBottom"`
		Viewport     float64 `json:"viewport"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const screen = document.querySelector('#login-screen');
		const button = document.querySelector('#form-login button[type="submit"]');

		// What a person does when a form runs off the edge, and what they cannot
		// do if the overlay does not scroll.
		screen.scrollTop = screen.scrollHeight;

		return {
			overflows: screen.scrollHeight > screen.clientHeight + 1,
			scrolled: screen.scrollTop,
			buttonBottom: button.getBoundingClientRect().bottom,
			viewport: window.innerHeight,
		};
	})())`, &out)

	if !out.Overflows {
		t.Skip("the sign-in screen fits in 320px, so there is nothing to scroll here")
	}

	if out.Scrolled == 0 {
		t.Fatal("the sign-in screen is taller than the window and will not scroll, " +
			"so whatever is past the edge cannot be reached at all")
	}

	if out.ButtonBottom > out.Viewport {
		t.Errorf("the submit button ends at %.0fpx in a %.0fpx window even after "+
			"scrolling to the bottom", out.ButtonBottom, out.Viewport)
	}
}

// Switching tabs has to switch what is on screen. A tab that highlights but
// changes nothing is a broken application with a healthy API.
//
// The built-in administrator's own tabs, which are the four it has: it does not record
// time, so the calendar, the entries and the projects are not on its screen at all.
// Deliberately still the plain sign-in rather than readyWorker - what this checks is
// that clicking a tab shows its panel, and it should stay the cheapest case in the
// suite rather than growing a password change and a second account.
func TestTabsSwitchTheVisiblePanel(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	// Out of the way: it is an overlay, so nothing behind it can be clicked.
	p.settleWizard()

	// And so is the walk through, which was the actual fault here.
	//
	// It opens by itself on a first sign-in - startTour runs whenever the account
	// has not seen it, and a fresh installation's built-in administrator has not.
	// Its bubble is a modal, so the tab clicks below landed on it instead, and no
	// amount of waiting afterwards helps: the click never reached the tab.
	//
	// Intermittent because it is a race rather than a rule. The tour opens after
	// the /me it waits on, so whether it is up when the first tab is clicked
	// depends on which arrives first - which is why this failed on two unrelated
	// pull requests and passed on the two beside them.
	p.settleWelcome()

	// And the load has to be finished, not merely far enough along to have drawn
	// the tabs. Signing in ends by choosing which screen to open on and putting
	// the reader back where a reload took them from - both of which switch the
	// view. A tab clicked before that lands, and is then switched away from, and
	// the wait below spends forty-five seconds on a panel that was up for a
	// moment. It reported the click as never having worked.
	p.settled()

	for _, view := range []struct{ tab, panel string }{
		{`.tab[data-view="roles"]`, "#view-roles"},
		{`.tab[data-view="admin"]`, "#view-admin"},
		{`.tab[data-view="settings"]`, "#view-settings"},
		{`.tab[data-view="users"]`, "#view-users"},
	} {
		p.run("switch to "+view.panel, chromedp.Click(view.tab, chromedp.ByQuery))

		// Waited for rather than slept through. This is a click and a class
		// change, so it is usually up within a frame - but "usually" is what a
		// fixed 150ms was betting on, and on a loaded runner it lost.
		p.waitShown(view.panel)
	}
}
