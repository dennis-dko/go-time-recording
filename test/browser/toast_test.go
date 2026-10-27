//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// A notice sits at the foot of the window; the same news again is counted on it
// rather than stacked under it; it holds still while the pointer is over it and
// goes once the pointer leaves and its time is up; it goes at once when it is
// closed; and a press on its words reaches what it lies over.
func TestANoticeIsCountedHeldAndSentAwayTheModernWay(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var placed struct {
		Bottom float64 `json:"bottom"`
		Height float64 `json:"height"`
		Notes  int     `json:"notes"`
		Count  string  `json:"count"`
	}

	p.awaitJSON(`(async () => {
		toast('Saved', 'ok');
		toast('Saved', 'ok');
		toast('Saved', 'ok');
		const box = document.querySelector('#toast .toast-note').getBoundingClientRect();
		return {
			bottom: box.bottom, height: window.innerHeight,
			notes: document.querySelectorAll('#toast .toast-note').length,
			count: document.querySelector('#toast .toast-count').textContent,
		};
	})()`, &placed)

	if placed.Notes != 1 || placed.Count != "×3" {
		t.Errorf("three notices of the same news made %d note(s) counting %q, want one counting ×3", placed.Notes, placed.Count)
	}

	if gap := placed.Height - placed.Bottom; gap < 0 || gap > 40 {
		t.Errorf("the notice ends %.0fpx above the foot of the window, want it at the foot", gap)
	}

	// For looking at, in the light and in the dark.
	p.run("a notice of each kind", chromedp.Evaluate(`toast('The server could not be reached. Check the connection and try again.', 'error')`, nil),
		chromedp.Sleep(400*time.Millisecond))
	p.saveShot(t, "toast-light.png")
	p.run("dark", chromedp.Evaluate(`document.documentElement.dataset.theme = 'dark'`, nil))
	p.saveShot(t, "toast-dark.png")
	p.run("clear them", chromedp.Evaluate(`document.querySelector('#toast').replaceChildren()`, nil))

	// Held by a real pointer over it: "Held" stays for a little over four
	// seconds, so six with the pointer there is past its time.
	var middle struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	p.evalJSON(`JSON.stringify((() => {
		toast('Held', 'ok');
		const box = document.querySelector('#toast .toast-note').getBoundingClientRect();
		return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
	})())`, &middle)

	p.run("the pointer onto the notice", chromedp.MouseEvent(input.MouseMoved, middle.X, middle.Y),
		chromedp.Sleep(6*time.Second))

	if p.count("#toast .toast-note") != 1 {
		t.Fatal("the notice left while the pointer was on it")
	}

	p.run("the pointer leaves", chromedp.MouseEvent(input.MouseMoved, 1, 1))
	p.waitGone("#toast .toast-note")

	// Gone at once, not after its time - which would come by itself within the
	// wait, and is what a check that waited for it could not tell apart.
	p.run("closed", chromedp.Evaluate(`toast('Closed', 'ok')`, nil), p.click("#toast .toast-close"),
		chromedp.Sleep(600*time.Millisecond))

	if p.count("#toast .toast-note") != 0 {
		t.Error("the closed notice is still up")
	}

	// The cross takes the press; the words do not, so whatever the notice lies
	// over - the footer, the last button of a form - is still pressed through it.
	var reached string

	p.run("put the pointer away", chromedp.MouseEvent(input.MouseMoved, 1, 1))
	p.evalJSON(`JSON.stringify((() => {
		toast('Over the page', 'ok');
		const words = document.querySelector('#toast .toast-text').getBoundingClientRect();
		const hit = document.elementFromPoint(words.left + words.width / 2, words.top + words.height / 2);
		return hit?.closest('#toast') ? 'the notice' : hit?.tagName ?? 'nothing';
	})())`, &reached)

	if reached == "the notice" {
		t.Error("a press on a notice's words is caught by the notice instead of what it lies over")
	}
}

// saveShot keeps a picture of the window beside the case, for a person to look at.
func (p *page) saveShot(t *testing.T, name string) {
	t.Helper()

	var shot []byte

	p.run("take "+name, chromedp.CaptureScreenshot(&shot))

	if err := os.WriteFile(filepath.Join(".", name), shot, 0o600); err != nil {
		t.Logf("could not keep %s: %v", name, err)
	}
}
