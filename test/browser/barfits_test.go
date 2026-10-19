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

	// A title of one long word, which cannot be folded at a space. It has to
	// give way inside the word rather than lie over what is beside it.
	const oneWord = "Arbeitszeiterfassungsportal"

	retitle := func() {
		t.Helper()

		p.run("a title of one long word", chromedp.Evaluate(fmt.Sprintf(
			`document.querySelector('#app-title-text').textContent = %q`, oneWord), nil))
	}

	for _, language := range []string{"en", "de"} {
		p.run("a desktop", chromedp.EmulateViewport(1600, 900))
		p.chooseLanguage(language)
		retitle()

		for _, width := range widths {
			p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 800))

			bar := p.readBar()
			where := fmt.Sprintf("a title of one long word, %s, %dpx", language, width)

			for _, overlap := range bar.Overlaps {
				t.Errorf("%s: %s", where, overlap)
			}

			if bar.PageWidth > bar.Screen+0.5 {
				t.Errorf("%s: the page is %.0fpx wide on a %.0fpx screen", where, bar.PageWidth, bar.Screen)
			}
		}
	}

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

// The account's name and role stay on one line of the bar, cut short where there
// is no room, and whole in the tooltip.
//
// The right-hand column of the bar holds the account and three controls, and its
// share of a laptop's width is little more than the controls need. The account
// was allowed to wrap, so it took what was left - the width of its longest word -
// and stood as a column of words: "System / Administrator / · / Administrator" at
// 1,024 pixels, six lines for a long name, and the bar that stays on top of every
// screen grew to 255 pixels. It keeps to one line now, at most 12em wide, which is
// what kept the bar no taller than before at every width measured; decided on
// 2026-10-17.
func TestTheAccountStaysOneLineOnTheBar(t *testing.T) {
	t.Parallel()

	const long = "Maximiliane Freifrau von und zu Beispielhausen"

	p := open(t)
	p.readyAdmin()

	var status string

	p.run("create an account with a long name", chromedp.Evaluate(`(async () => {
		const csrf = document.cookie.split(';').map(c => c.trim())
			.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
		const r = await fetch('/api/v1/users', {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			body: JSON.stringify({ name: '`+long+`', email: 'long@example.com', role: 'user-admin',
				password: 'a-long-enough-password' }),
		});
		return String(r.status);
	})()`, &status, awaitPromise))

	if status != "201" && status != "200" {
		t.Fatalf("could not create the account: HTTP %s", status)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("long@example.com", "a-long-enough-password")
	p.waitGone("#login-screen")
	p.settleWelcome()

	for _, language := range []string{"de", "en"} {
		p.run("a desktop", chromedp.EmulateViewport(1600, 900))
		p.chooseLanguage(language)

		for _, width := range []int64{1440, 1280, 1100, 1024, 960, 901} {
			p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 800))

			var who struct {
				Lines float64 `json:"lines"`
				Cut   bool    `json:"cut"`
				Said  string  `json:"said"`
				Title string  `json:"title"`
			}

			p.evalJSON(`JSON.stringify((() => {
				const who = document.querySelector('#who');
				const line = parseFloat(getComputedStyle(who).lineHeight) || 16;

				return {
					lines: who.getBoundingClientRect().height / line,
					cut: who.scrollWidth > who.clientWidth + 1,
					said: who.textContent,
					title: who.title,
				};
			})())`, &who)

			if who.Lines > 1.5 {
				t.Errorf("%s, %dpx: the account takes %.1f lines of the bar: %q", language, width, who.Lines, who.Said)
			}

			if who.Cut && who.Title != who.Said {
				t.Errorf("%s, %dpx: the account is cut short and its tooltip says %q rather than %q",
					language, width, who.Title, who.Said)
			}
		}
	}
}

// Where the bar has room, the account is shown whole.
//
// The account is held to one line and cut short where the bar has no room for it
// - at 12em, which is what keeps a laptop's bar from growing. Held to 12em
// everywhere, it was cut on a wide screen as well, where the room was plainly
// there: "Erika · Benutzer & Admini…" at 1,920 and at 2,560 pixels. From 1,440
// pixels, where a measured bar has room to spare, it may take 20em.
func TestWhereTheBarHasRoomTheAccountIsShownWhole(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "erika@example.com", "a-long-enough-password", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))
	p.signIn("erika@example.com", "a-long-enough-password")
	p.waitGone("#login-screen")
	p.settleWelcome()
	p.chooseLanguage("de")

	for _, width := range []int64{1920, 1600, 1440} {
		p.run(fmt.Sprintf("%dpx", width), chromedp.EmulateViewport(width, 900))

		var who struct {
			Cut  bool   `json:"cut"`
			Said string `json:"said"`
		}

		p.evalJSON(`JSON.stringify((() => {
			const who = document.querySelector('#who');

			return { cut: who.scrollWidth > who.clientWidth + 1, said: who.textContent };
		})())`, &who)

		if who.Cut {
			t.Errorf("at %dpx the bar cuts the account short although it has room: %q", width, who.Said)
		}
	}
}
