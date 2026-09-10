//go:build browser

package browser

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Each place can be given a different part of the logo.
//
// A logo is uploaded once and drawn in three places that want three different
// things. A wide header takes the whole wordmark; a browser tab cannot - sixteen
// pixels of a two-to-one wordmark is a smear, and what is worth keeping there is
// usually the mark at one end. Nobody can guess which end, so it is chosen.
//
// The selection keeps the shape of the place it is for, so what is on this screen
// is what will be on the others: a free-form one would be a selection that cannot
// be used as chosen.
func TestAPartOfTheLogoCanBeChosenForEachPlace(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// The previews are controls, and each one opens the chooser for its own place.
	if p.count(".logo-use-button") != 3 {
		t.Fatalf("%d previews can be pressed, want one per place",
			p.count(".logo-use-button"))
	}

	p.run("choose the part used in the tab",
		p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))

	// Square, because a tab is. The box is the application's answer to "as much
	// of this as a square can hold", before anything is dragged.
	var box struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const r = document.querySelector('#crop-box').getBoundingClientRect();
		return { width: r.width, height: r.height };
	})())`, &box)

	if box.Width < 8 || box.Height < 8 {
		t.Fatalf("the selection opened at %.0fx%.0f, which cannot be aimed at",
			box.Width, box.Height)
	}

	if ratio := box.Width / box.Height; ratio < 0.9 || ratio > 1.1 {
		t.Errorf("the tab's selection is %.0fx%.0f, a ratio of %.2f - a tab is square",
			box.Width, box.Height, ratio)
	}

	// Moved to the left end of the logo, which is where a wordmark keeps its
	// mark, and stored.
	p.run("take the left end", chromedp.Evaluate(
		`(() => {
			cropBox = { ...cropBox, x: 0, y: 0 };
			drawCropBox();
			document.querySelector('#crop-apply').click();

			return 1;
		})()`, nil))

	// What the page believes it chose, before saving - so a failure says which of
	// the two halves broke rather than only that the answer is empty.
	var chosen string

	p.run("read the chosen part", chromedp.Evaluate(
		`JSON.stringify(logoCrops)`, &chosen))

	if !strings.Contains(chosen, "icon") {
		t.Fatalf("the chooser stored nothing for the tab: %s", chosen)
	}

	// What the request actually carried, recorded as it goes: the page is where
	// this was failing, and the body it sends is the one thing neither side's log
	// shows.
	// Kept in the session rather than on the window: the save reloads the page,
	// and everything this document was holding goes with it.
	p.run("watch the save", chromedp.Evaluate(`
		(() => {
			sessionStorage.removeItem('__sent');
			const real = window.fetch;

			window.fetch = (url, options) => {
				if (String(url).includes('/settings/branding')) {
					sessionStorage.setItem('__sent', options?.body ?? '');
				}

				return real(url, options);
			};

			return 1;
		})()`, nil))

	p.submitAndAwaitReload(t, p.click(`#form-branding button[type="submit"]`))

	var sent string

	p.run("read what was sent", chromedp.Evaluate(
		`String(sessionStorage.getItem('__sent') ?? 'nothing was sent')`, &sent))

	if !strings.Contains(sent, "crops") {
		t.Fatalf("the save carried no crops: %.300s", sent)
	}

	// What was chosen comes back, so the chooser reopens on it rather than
	// starting again - and the server has it, which is what the three sizes are
	// made from.
	var stored struct {
		Icon struct {
			X float64 `json:"x"`
			W float64 `json:"w"`
		} `json:"icon"`
	}

	var raw string

	p.run("read what was stored", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/branding', { credentials: 'same-origin' });
			const body = await r.json();

			return JSON.stringify(body?.data?.crops ?? {});
		})()`, &raw, awaitPromise))

	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("reading what was stored: %v; %s", err, raw)
	}

	if stored.Icon.W <= 0 {
		t.Fatal("nothing was stored for the tab's part of the logo")
	}

	if stored.Icon.W >= 1 {
		t.Errorf("the whole logo was stored (w=%.2f) rather than the part chosen",
			stored.Icon.W)
	}

	if stored.Icon.X > 0.05 {
		t.Errorf("the part stored starts at %.2f rather than at the left end",
			stored.Icon.X)
	}
}

// The selection is free: a corner pulled in one direction moves in that
// direction only, and whatever shape comes out of it is what gets stored.
//
// The opening selection has the shape of the place it is for, and that used to
// be a rule rather than a starting point - a corner dragged upwards took the
// width with it, so a tab could only ever be given a square of the logo. This is
// the test that the rule is gone, dragged with real pointer input so that a
// corner nothing is wired to fails here rather than passing.
func TestTheChosenPartCanBeAnyShape(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("choose the part used in the tab",
		p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))

	before := p.selection(t)

	// The logo is five times as wide as it is tall, so the square the tab opens
	// on is the full height of it - which is what makes pulling the bottom edge
	// up a fair question to ask.
	if before.H < 0.9 {
		t.Fatalf("the tab's selection opened %.2f of the way down the logo, "+
			"so there is nothing to pull up", before.H)
	}

	p.drag(`.crop-handle-se`, 0, -60)

	after := p.selection(t)

	if after.H >= before.H-0.1 {
		t.Fatalf("the bottom edge was pulled up and the selection is still "+
			"%.2f tall, was %.2f", after.H, before.H)
	}

	// The point of the whole exercise. Under the old rule this width would have
	// followed the height down to keep the square square.
	if math.Abs(after.W-before.W) > 0.02 {
		t.Errorf("pulling the bottom edge up changed the width from %.2f to %.2f; "+
			"a corner should move the two edges it is dragged along and no others",
			before.W, after.W)
	}

	// Still on the logo. A selection that has been dragged off it describes a
	// part that does not exist, which the server would have to guess about.
	if after.X < 0 || after.Y < 0 || after.X+after.W > 1.001 || after.Y+after.H > 1.001 {
		t.Errorf("the selection left the logo: %+v", after)
	}

	p.run("use this part", p.click("#crop-apply"))
	p.submitAndAwaitReload(t, p.click(`#form-branding button[type="submit"]`))

	// And it survives being stored: the shape that was chosen is the shape that
	// comes back, rather than one the server rounded to something it preferred.
	var stored struct {
		Icon struct {
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"icon"`
	}

	var raw string

	p.run("read what was stored", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/branding', { credentials: 'same-origin' });
			const body = await r.json();

			return JSON.stringify(body?.data?.crops ?? {});
		})()`, &raw, awaitPromise))

	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("reading what was stored: %v; %s", err, raw)
	}

	if math.Abs(stored.Icon.H-after.H) > 0.02 || math.Abs(stored.Icon.W-after.W) > 0.02 {
		t.Errorf("chose %.2fx%.2f and %.2fx%.2f came back",
			after.W, after.H, stored.Icon.W, stored.Icon.H)
	}
}

// selection is the part of the logo the chooser currently has marked, in
// fractions of the whole image.
func (p *page) selection(t *testing.T) struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
} {
	t.Helper()

	var box struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}

	// Read off the screen rather than out of the page's own variable: what is
	// drawn is what is being aimed at, and the two agreeing is part of what is
	// under test.
	p.evalJSON(`JSON.stringify((() => {
		const image = document.querySelector('#crop-image');
		const area = drawnImageArea(image);
		const box = document.querySelector('#crop-box').getBoundingClientRect();
		const stage = document.querySelector('#crop-stage').getBoundingClientRect();

		return {
			x: (box.left - stage.left - area.left) / area.width,
			y: (box.top - stage.top - area.top) / area.height,
			w: box.width / area.width,
			h: box.height / area.height,
		};
	})())`, &box)

	return box
}
