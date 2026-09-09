//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The appearance, the language and the way out fold away with the navigation.
//
// They are four controls beside an account's name, and the longest of them
// reads "Automatic (by time of day)" - wider than a telephone on their own, so
// on a narrow screen they wrapped into two further lines of a bar that is at
// the top of every screen and stays there. Measured on a 390px screen before
// this: the bar came to 190px, a quarter of the window, on a screen whose
// navigation had already been folded away for the same reason.
//
// So they go where the navigation went: behind the burger, which is what the
// burger is for. What stays is the account's name, because that is the one
// thing on the bar that answers a question somebody has without pressing
// anything.
func TestTheAccountControlsFoldIntoTheBurgerOnAPhone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	folding := []string{"#theme-picker", "#language-picker", "#logout"}

	// Wide: all of them on the bar, where they have always been.
	p.run("a wide window", chromedp.EmulateViewport(1280, 900))

	for _, control := range folding {
		if !p.visible(control) {
			t.Errorf("%s is not on the bar of a 1280px window", control)
		}
	}

	if p.visible("#nav-toggle") {
		t.Error("a wide screen offers a burger for a bar that is already open")
	}

	// Narrow: folded away, and the bar is shorter for it.
	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	for _, control := range folding {
		if p.visible(control) {
			t.Errorf("%s is still on the bar of a 390px screen, where it does not fit",
				control)
		}
	}

	if !p.visible("#who") {
		t.Error("the bar no longer says who is signed in")
	}

	// And the room it saves is the point of it, measured against the same bar
	// rather than against a number.
	//
	// This asked whether the bar came to less than a fifth of the window, which
	// is a bound on the wrong thing: how tall the bar is depends on how the
	// account's name wraps, and that depends on the fonts the machine has. It
	// held here at 109px and failed on the CI runner at 161, where "System
	// Administrator" takes two lines - a case that passes on the machine it was
	// written on and fails on the one that gates the release, over a difference
	// that is not about the change at all.
	//
	// So the controls are put back for a moment, by hand, and the bar measured
	// again. The difference between the two is exactly what folding them away
	// buys, on whatever machine is asking.
	folded := p.pixels(".topbar", "offsetHeight")

	p.run("show them again for a moment", chromedp.Evaluate(
		`document.querySelector('#topbar-controls').style.display = 'flex'`, nil))

	shown := p.pixels(".topbar", "offsetHeight")

	// Against one of the controls rather than against a pixel count, which is
	// the same trap one step further in: the bar grew by 81px here, and a bound
	// of eighty would have been a second number that happened to hold on this
	// machine. What has to be true is that the bar loses a control's worth of
	// height - that they are folded away rather than merely quiet inside a bar
	// that stays as tall.
	picker := p.pixels("#theme-picker", "offsetHeight")

	p.run("and fold them back", chromedp.Evaluate(
		`document.querySelector('#topbar-controls').style.removeProperty('display')`, nil))

	if shown-folded < picker {
		t.Errorf("folding them away saves %dpx, less than the %dpx the appearance "+
			"picker alone is tall: the bar is %dpx folded and %dpx with them on it",
			shown-folded, picker, folded, shown)
	}

	// Opening the burger brings them within reach.
	p.run("open the menu", p.click("#nav-toggle"))

	for _, control := range folding {
		if !p.visible(control) {
			t.Errorf("%s cannot be reached from the open menu on a telephone", control)
		}
	}

	// And opening it did not make the page draggable sideways, which is what the
	// navigation's own fold was about.
	var over float64

	p.evalJSON(`JSON.stringify(
		document.documentElement.scrollWidth - document.documentElement.clientWidth)`, &over)

	if over > 1 {
		t.Errorf("the open menu is %.0fpx wider than the window", over)
	}

	// The burger says what it opens. It is two regions now - the points and the
	// controls beside them - and a control that names only half of what it opens
	// tells a screen reader half of it.
	names := p.attr("#nav-toggle", "aria-controls")
	for _, region := range []string{"tabs", "topbar-controls"} {
		if !strings.Contains(names, region) {
			t.Errorf("the burger's aria-controls is %q, which does not name %q",
				names, region)
		}
	}

	// Using one of them leaves the menu open: three controls behind one press,
	// and a menu that shuts on the first of them makes the other two two more
	// presses each. The tabs close it on purpose - choosing a point is the end of
	// what the menu was opened for - and these are not tabs.
	p.run("choose dark", p.chooseOption("#theme-picker", "dark"))

	if p.attr("#nav-toggle", "aria-expanded") != "true" {
		t.Error("choosing an appearance closed the menu the language is still in")
	}

	// The way out works from in there, which is the whole point of it being in
	// there.
	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#form-login") {
		t.Error("signing out from the folded menu did not reach the sign-in screen")
	}
}
