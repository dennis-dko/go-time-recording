//go:build browser

package browser

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// barReading is where the four things on the bar's first row are, and how wide
// the page has become.
type barReading struct {
	Overlaps  []string `json:"overlaps"`
	PageWidth float64  `json:"pageWidth"`
	Screen    float64  `json:"screen"`
	Folded    bool     `json:"folded"`
}

// readBar measures the bar once the page has drawn at its new width.
func (p *page) readBar() barReading {
	p.t.Helper()

	var reading barReading

	p.run("let the page draw", chromedp.Evaluate(
		`new Promise((drawn) => requestAnimationFrame(() => requestAnimationFrame(() => drawn(true))))`,
		nil, awaitPromise))

	p.evalJSON(`JSON.stringify((() => {
		const visible = (node) => node && node.getBoundingClientRect().width > 0;
		const mark = [document.querySelector('#brand-logo'), document.querySelector('#brand-mark')].find(visible);

		const parts = {
			'the mark': mark,
			'the title': document.querySelector('#app-title'),
			'the name': document.querySelector('#who'),
			'the controls': document.querySelector('#topbar-controls'),
		};

		const boxes = Object.entries(parts)
			.filter(([, node]) => visible(node))
			.map(([name, node]) => [name, node.getBoundingClientRect()]);

		const overlaps = [];

		for (let i = 0; i < boxes.length; i++) {
			for (let j = i + 1; j < boxes.length; j++) {
				const [a, first] = boxes[i];
				const [b, second] = boxes[j];

				const across = Math.min(first.right, second.right) - Math.max(first.left, second.left);
				const down = Math.min(first.bottom, second.bottom) - Math.max(first.top, second.top);

				if (across > 0.5 && down > 0.5) overlaps.push(a + ' and ' + b + ' by ' + Math.round(across) + 'px');
			}
		}

		return {
			overlaps,
			pageWidth: document.documentElement.scrollWidth,
			screen: document.documentElement.clientWidth,
			folded: !visible(document.querySelector('#topbar-controls')),
		};
	})())`, &reading)

	return reading
}

// Nothing on the bar is drawn over anything else, at any width a desktop has.
//
// The bar's first row is three columns, the outer two equal so that the title
// sits in the middle. The right one holds the account's name and three
// controls, and it needs more than its share on any window narrower than about
// 1,300 pixels - which is most laptops. It was allowed to shrink below what it
// holds, so what it holds hung out of it to the left, where the title is: the
// name was drawn over the title, letter on letter. Measured at 1,100 pixels in
// German, the whole of the name lay inside the title's box.
//
// A telephone was never affected, because below 900 pixels the controls fold
// away behind the burger. Between that and a wide screen is where nothing had
// looked.
func TestNothingOnTheBarIsDrawnOverAnythingElse(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "bothe@example.com", "both-jobs-password-1", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("bothe@example.com", "both-jobs-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	widths := []int64{1440, 1366, 1280, 1200, 1100, 1024, 960, 901}

	measure := func(what string) {
		t.Helper()

		for _, language := range []string{"en", "de"} {
			p.run("a desktop", chromedp.EmulateViewport(1600, 900))
			p.chooseLanguage(language)

			for _, name := range []string{"", "Maximiliane Freifrau von und zu Beispielhausen"} {
				if name != "" {
					// A name the installation did not choose and cannot shorten.
					p.run("a long name", chromedp.Evaluate(fmt.Sprintf(
						`document.querySelector('#who strong').textContent = %q`, name), nil))
				}

				for _, width := range widths {
					p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 800))

					bar := p.readBar()
					where := fmt.Sprintf("%s, %s, %dpx, name %q", what, language, width, name)

					for _, overlap := range bar.Overlaps {
						t.Errorf("%s: %s", where, overlap)
					}

					if bar.PageWidth > bar.Screen+0.5 {
						t.Errorf("%s: the page is %.0fpx wide on a %.0fpx screen", where, bar.PageWidth, bar.Screen)
					}
				}
			}
		}
	}

	measure("the placeholder mark")

	// And with the widest thing the left end can hold: a logo five times as wide
	// as it is high, which the bar shows at its full 328 pixels where it can.
	p.run("a desktop", chromedp.EmulateViewport(1600, 900))
	p.storeBranding(t, wideLogo)
	p.reload()

	if !p.visible("#brand-logo") {
		t.Fatal("the logo that was stored is not on the bar")
	}

	measure("a wide logo")
}
