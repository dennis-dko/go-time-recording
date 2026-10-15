//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Signing out on a telephone leaves nothing of the application on the screen.
//
// The sign-in screen covers the page down to the footer, and the footer is
// raised above the rest, so between the two of them they are the whole window -
// by geometry, which a telephone does not keep. Firefox for Android moves what
// is fixed to the bottom of the window with its toolbar and leaves what is fixed
// at both ends where it was, and the strip that opened between the sign-in
// screen and the footer showed the bottom of the time entries screen that had
// just been signed out of.
//
// Chrome keeps the two edges together, so the strip is opened here by hand -
// the footer's reservation made taller than the footer - and stands in for the
// toolbar. What is asked is the same either way: under every point of the
// window there is the sign-in screen, the footer or the page's own background,
// and nothing of the application.
func TestSigningOutOnAPhoneLeavesNothingOfTheApplicationOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the time entries", p.click(`.tab[data-view="timesheets"]`))
	p.waitShown("#workbook-card")

	p.run("a telephone", chromedp.EmulateViewport(390, 760))

	// Where somebody is when they reach for the way out: the end of a long screen.
	p.run("scroll to the end", chromedp.Evaluate(
		`window.scrollTo(0, document.documentElement.scrollHeight)`, nil))

	p.run("sign out through the fold", p.click("#nav-toggle"), p.click("#logout"))
	p.waitShown("#login-screen")
	p.waitEvaluates("the sign-in form is showing",
		`String(!document.querySelector('#login-screen').classList.contains('checking'))`, "true")

	const showing = `JSON.stringify((() => {
		const screen = document.querySelector('#login-screen');
		const footer = document.querySelector('#site-footer');
		const found = [];

		for (let y = 1; y < window.innerHeight; y += 4) {
			const hit = document.elementFromPoint(window.innerWidth / 2, y);
			const allowed = hit === null || hit === document.body ||
				hit === document.documentElement || screen.contains(hit) || footer.contains(hit);

			if (!allowed) {
				found.push(y + 'px: ' + hit.tagName.toLowerCase() +
					(hit.id ? '#' + hit.id : '') + ' "' + (hit.textContent || '').trim().slice(0, 40) + '"');
			}
		}

		return found;
	})())`

	var uncovered []string

	p.evalJSON(showing, &uncovered)

	if len(uncovered) > 0 {
		t.Errorf("with the sign-in screen up, %d points down the middle of a 390px "+
			"window show the application behind it, the first at %s", len(uncovered), uncovered[0])
	}

	// The strip a telephone's toolbar opens.
	p.run("open a strip between the sign-in screen and the footer", chromedp.Evaluate(`(() => {
		const footer = document.querySelector('#site-footer').getBoundingClientRect().height;
		document.documentElement.style.setProperty('--footer-height', (footer + 60) + 'px');
	})()`, nil))

	p.evalJSON(showing, &uncovered)

	if len(uncovered) > 0 {
		t.Errorf("with a strip open between the sign-in screen and the footer, %d points "+
			"show the application that was signed out of, the first at %s",
			len(uncovered), uncovered[0])
	}
}
