//go:build browser

package browser

import (
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
