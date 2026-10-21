//go:build browser

package browser

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The version belongs in the corner of every page, and the footer used to be
// hidden whenever no branding was configured.
func TestTheFooterShowsTheRunningVersion(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	if !p.visible("#site-footer") {
		t.Fatal("the footer is hidden, so the version has nowhere to appear")
	}

	version := strings.TrimSpace(p.text("#footer-version"))
	if version == "" {
		t.Fatal("the footer shows no version")
	}

	// And the platform beside it, as "v1.2.0 (linux)". The same version is
	// published for four platforms and they do not all behave alike - restarting
	// from the interface works here and cannot on Windows - so the version alone
	// does not say what somebody is looking at.
	//
	// Against the platform this test is running on rather than a hard-coded
	// "linux". CI is Linux and a developer's machine is whatever it is - asserting
	// the former made the suite fail on Windows for saying something true, which
	// trains whoever runs it locally to expect red and stop reading it.
	if !strings.Contains(version, "(") || !strings.Contains(version, ")") {
		t.Errorf("the footer shows %q, without the platform in brackets", version)
	}

	if want := "(" + runtime.GOOS + ")"; !strings.Contains(version, want) {
		t.Errorf("the footer shows %q, want %s - the platform the application is "+
			"actually running on", version, want)
	}

	// The version itself is still in front of it, rather than having been
	// replaced by the platform.
	if strings.HasPrefix(version, "(") {
		t.Errorf("the footer shows only the platform: %q", version)
	}
}

// The other corner holds the mark of where this comes from, linked to it.
//
// Level with the version and the same distance in from its own edge, so the two
// corners read as a pair rather than as one deliberate thing and one that landed
// where it landed.
func TestTheFooterLinksToTheSource(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	if !p.visible("#footer-source") {
		t.Fatal("the footer has no link to the source")
	}

	if href := p.attr("#footer-source", "href"); !strings.Contains(href, "github.com/") {
		t.Errorf("the source link points at %q", href)
	}

	// A link that is a picture and nothing else says nothing to a screen reader,
	// and nothing to anybody pointing at it wondering what it is.
	if label := p.attr("#footer-source", "aria-label"); strings.TrimSpace(label) == "" {
		t.Error("the mark is unlabelled, so it is a link to nowhere named")
	}

	// Opened away from the application rather than in place of it. Somebody
	// reading the source has not finished with the screen they were on.
	if got := p.attr("#footer-source", "target"); got != "_blank" {
		t.Errorf("the source link opens in %q, taking the application's place", got)
	}

	var corners struct {
		SourceLeft   float64 `json:"sourceLeft"`
		SourceMiddle float64 `json:"sourceMiddle"`
		SourceHeight float64 `json:"sourceHeight"`
		VersionRight float64 `json:"versionRight"`
		VersionMid   float64 `json:"versionMiddle"`
		FooterLeft   float64 `json:"footerLeft"`
		FooterRight  float64 `json:"footerRight"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const source = document.querySelector('#footer-source').getBoundingClientRect();
		const version = document.querySelector('#footer-version').getBoundingClientRect();
		const footer = document.querySelector('#site-footer').getBoundingClientRect();

		return {
			sourceLeft: source.left - footer.left,
			sourceMiddle: source.top + source.height / 2,
			sourceHeight: source.height,
			versionRight: footer.right - version.right,
			versionMiddle: version.top + version.height / 2,
			footerLeft: footer.left,
			footerRight: footer.right,
		};
	})())`, &corners)

	if math.Abs(corners.SourceMiddle-corners.VersionMid) > 2 {
		t.Errorf("the mark sits %.0fpx down the footer and the version %.0fpx, so "+
			"they are not on the same line", corners.SourceMiddle, corners.VersionMid)
	}

	if math.Abs(corners.SourceLeft-corners.VersionRight) > 2 {
		t.Errorf("the mark is %.0fpx in from the left and the version %.0fpx in from "+
			"the right", corners.SourceLeft, corners.VersionRight)
	}

	// Big enough to aim at and to recognise. The version's line is about this
	// tall, which is what "the same height" means here.
	if corners.SourceHeight < 14 || corners.SourceHeight > 24 {
		t.Errorf("the mark is %.0fpx tall", corners.SourceHeight)
	}
}

// The footer sits at the bottom of the window, not at the end of the content.
//
// "My account" is a few short cards, and the footer followed them - which reads
// as the page having stopped early, with a band of empty ground under it. And it
// was as tall as whatever happened to be in it, so it was a different strip on
// every screen.
func TestTheFooterStaysAtTheBottomAtOneHeight(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	measure := func() struct {
		Bottom float64 `json:"bottom"`
		Height float64 `json:"height"`
		Window float64 `json:"window"`
	} {
		var out struct {
			Bottom float64 `json:"bottom"`
			Height float64 `json:"height"`
			Window float64 `json:"window"`
		}

		p.evalJSON(`JSON.stringify((() => {
			const r = document.querySelector('#site-footer').getBoundingClientRect();

			return { bottom: r.bottom, height: r.height, window: window.innerHeight };
		})())`, &out)

		return out
	}

	// A window taller than the screen's content, or the question cannot be asked:
	// where the content already fills the window the footer is at the bottom of
	// both, and every layout looks right.
	p.run("a tall window", chromedp.EmulateViewport(1280, 1600))

	p.run("open the shortest screen", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-password", chromedp.ByID))

	short := measure()

	if short.Window < 1000 {
		t.Fatalf("the window is only %.0fpx tall, so this proves nothing", short.Window)
	}

	if short.Bottom < short.Window-2 {
		t.Errorf("on a short screen the footer ends %.0fpx down a %.0fpx window, "+
			"leaving empty ground under it", short.Bottom, short.Window)
	}

	// And the same strip on a screen with plenty on it.
	p.run("open a longer one", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	long := measure()

	if math.Abs(long.Height-short.Height) > 2 {
		t.Errorf("the footer is %.0fpx tall on one screen and %.0fpx on another",
			short.Height, long.Height)
	}

	if short.Height < 40 || short.Height > 80 {
		t.Errorf("the footer is %.0fpx tall", short.Height)
	}
}

// The footer is on the sign-in screen, where it used to be painted over.
//
// The sign-in screen is a fixed, opaque cover over the whole window, and the
// footer is the last thing in the page beneath it - so the version and the link
// to the source were on every screen except the one everybody sees first, and
// the one where "which version is this" is asked before anybody can sign in to
// find out.
//
// Asked of what is on top at each one's own centre rather than of visibility:
// checkVisibility reports an element that is painted over as visible, which is
// exactly how this stayed unnoticed.
func TestTheSignInScreenShowsTheFooter(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.run("wait for the sign-in form", chromedp.WaitVisible("#form-login", chromedp.ByID))

	// The version arrives with the branding, which is a request of its own.
	p.waitEvaluates("the version in the footer",
		`String(document.querySelector('#footer-version')?.textContent.trim() !== '')`, "true")

	for _, selector := range []string{"#footer-source", "#footer-version"} {
		var onTop bool

		p.run("find what is on top of "+selector, chromedp.Evaluate(fmt.Sprintf(`(() => {
			const el = document.querySelector(%q);
			const r = el.getBoundingClientRect();
			if (r.width === 0 || r.height === 0) return false;

			const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);

			return hit !== null && (hit === el || el.contains(hit));
		})()`, selector), &onTop))

		if !onTop {
			t.Errorf("%s is covered on the sign-in screen, so nobody signing in can "+
				"see or reach it", selector)
		}
	}

	// And the card ends where the footer starts, or above it. The screen scrolls on
	// a short window so the submit button stays reachable; a footer laid over the
	// card would put that button underneath it instead. The footer is inside the
	// screen now, after the card, so it is the card's edge that is asked about.
	var edges struct {
		Card   float64 `json:"card"`
		Footer float64 `json:"footer"`
	}

	p.evalJSON(`JSON.stringify({
		card: document.querySelector('#form-login').getBoundingClientRect().bottom,
		footer: document.querySelector('#site-footer').getBoundingClientRect().top,
	})`, &edges)

	if edges.Card > edges.Footer+1 {
		t.Errorf("the sign-in card reaches %.0fpx and the footer starts at %.0fpx, "+
			"so the bottom of the card is behind the footer", edges.Card, edges.Footer)
	}
}

// On a telephone the mark keeps to its own place beside the footer's text.
//
// The corners are taken out of the flow on a wide screen and put back into it on
// a narrow one. The mark went back as position: relative, so that the room for a
// finger around it has something to be placed against - and relative is the one
// position that lets the wide screen's left and top move it: it sat 20 pixels to
// the right of its place, on the first letters of the copyright line. Nothing had
// measured a narrow footer with text in it; the harness configures none, and
// alone on its line the mark overlapped nothing.
func TestOnAPhoneTheSourceMarkKeepsOffTheFooterText(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.run("a telephone", chromedp.EmulateViewport(390, 844))
	p.waitShown("#footer-source")

	// The line an installation writes there, which the harness leaves empty.
	p.run("a footer line", chromedp.Evaluate(`(() => {
		const text = document.querySelector('#footer-text');
		text.hidden = false;
		text.textContent = 'Copyright @ 2026 Dennis Köhler';
		return 1;
	})()`, nil))

	var drawn struct {
		Overlap float64 `json:"overlap"`
		Mark    string  `json:"mark"`
		Text    string  `json:"text"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const mark = document.querySelector('#footer-source').getBoundingClientRect();
		const text = document.querySelector('#footer-text').getBoundingClientRect();
		const across = Math.min(mark.right, text.right) - Math.max(mark.left, text.left);
		const down = Math.min(mark.bottom, text.bottom) - Math.max(mark.top, text.top);
		const span = (r) => r.left.toFixed(0) + '..' + r.right.toFixed(0) + ' x ' + r.top.toFixed(0) + '..' + r.bottom.toFixed(0);

		return { overlap: across > 0 && down > 0 ? across : 0, mark: span(mark), text: span(text) };
	})())`, &drawn)

	if drawn.Overlap > 0 {
		t.Errorf("on a telephone the source mark (%s) lies %.0fpx over the footer's text (%s)",
			drawn.Mark, drawn.Overlap, drawn.Text)
	}
}

// On a short window the footer comes after the sign-in form rather than over the
// window's bottom.
//
// The footer was raised above the sign-in screen at the bottom of the window,
// and the screen stopped where the footer began. With a legal notice in it, the
// footer was 135 pixels of a telephone held sideways: 185 of its 320 were left
// for the form, which scrolled in what remained and began below them - the
// installation's name and the first field in view, the password and every button
// to be found by scrolling a strip a third of the window high. On a 320 by 568
// telephone held upright the notice ran to four lines and the third way to sign in
// sat under the fold of what the footer left.
//
// The installation is given a footer line and a legal notice, because the
// harness configures none, and a footer of one short line hid this everywhere.
func TestOnAShortWindowTheFooterComesAfterTheSignInForm(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.awaitJSON(`api('/settings/branding', {method: 'PUT', body: JSON.stringify({
		footerText: 'Example Ltd · Main Street 1 · 12345 Sample Town',
		legalNotice: 'Example Ltd, Main Street 1, 12345 Sample Town. Registered at the district court of Sample Town, HRB 12345.',
	})}).then(() => true)`, nil)

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// Loaded again, as the next person to arrive finds it: the page read the
	// branding when it opened, before the notice was configured.
	p.reloadSignedOut()
	p.waitEvaluates("the legal notice in the footer",
		`String(document.querySelector('#footer-legal').textContent.trim() !== '')`, "true")

	type reading struct {
		Window      float64 `json:"window"`
		FooterTop   float64 `json:"footerTop"`
		CardBottom  float64 `json:"cardBottom"`
		VersionSeen bool    `json:"versionSeen"`
	}

	read := func(where string) reading {
		var r reading

		p.evalJSON(`(() => {
			const screen = document.querySelector('#login-screen');
			screen.scrollTop = `+where+`;
			const footer = document.querySelector('#site-footer').getBoundingClientRect();
			const version = document.querySelector('#footer-version').getBoundingClientRect();
			const hit = document.elementFromPoint(version.left + version.width / 2, version.top + version.height / 2);

			return JSON.stringify({
				window: window.innerHeight,
				footerTop: footer.top,
				cardBottom: document.querySelector('#form-login').getBoundingClientRect().bottom,
				versionSeen: hit !== null && hit.closest('#footer-version') !== null,
			});
		})()`, &r)

		return r
	}

	for _, size := range []struct{ w, h int64 }{{568, 320}, {667, 375}, {844, 390}, {320, 568}} {
		p.run(fmt.Sprintf("%dx%d", size.w, size.h), chromedp.EmulateViewport(size.w, size.h))
		p.run("let the page draw", chromedp.Evaluate(
			`new Promise((drawn) => requestAnimationFrame(() => requestAnimationFrame(() => drawn(true))))`,
			nil, awaitPromise))

		top := read("0")

		// Cut where the footer begins: what is left of the form is under it.
		if top.CardBottom > top.FooterTop+1 {
			t.Errorf("at %dx%d the form is cut off %.0fpx from the top, where the footer takes "+
				"the window's bottom %.0fpx", size.w, size.h, top.FooterTop, top.Window-top.FooterTop)
		}

		end := read("1e6")

		if !end.VersionSeen {
			t.Errorf("at %dx%d the footer cannot be reached by scrolling to the end of the screen",
				size.w, size.h)
		}

		if end.CardBottom > end.FooterTop+1 {
			t.Errorf("at %dx%d the form ends at %.0fpx and the footer begins at %.0fpx, over it",
				size.w, size.h, end.CardBottom, end.FooterTop)
		}
	}

	// And a window with room keeps the footer at its bottom edge, where it was.
	p.run("a laptop", chromedp.EmulateViewport(1280, 800))

	var bottom struct {
		Footer float64 `json:"footer"`
		Window float64 `json:"window"`
	}

	p.evalJSON(`JSON.stringify({
		footer: document.querySelector('#site-footer').getBoundingClientRect().bottom,
		window: window.innerHeight,
	})`, &bottom)

	if math.Abs(bottom.Footer-bottom.Window) > 1 {
		t.Errorf("on a laptop the footer ends at %.0fpx of a %.0fpx window", bottom.Footer, bottom.Window)
	}
}
