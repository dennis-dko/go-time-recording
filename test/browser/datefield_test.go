//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/device"
)

// A date field reads and accepts the reader's own convention.
//
// <input type="date"> renders in the browser's UI language and ignores the page
// entirely, so a German screen on an English machine asked for a date
// American-first and an English screen on a German one asked TT.MM.JJJJ. The
// native field is kept - it is the picker, and on a phone it is the date wheel -
// and what is on screen beside it is written and read here.
//
// Only a browser can answer any of this: the format is the browser's own Intl,
// the typing is a real keyboard, and whether the two halves agree is a fact
// about a running page.
func TestADateFieldIsWrittenTheReadersWay(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	read := func() (shown, iso, hint string) {
		var raw string

		p.run("read the field", chromedp.Evaluate(`
			(() => {
				const w = document.querySelector('#form-timesheet .date-wrap');
				const s = w.querySelector('.date-shown');
				return [s.value, w.querySelector('.date-native').value, s.placeholder]
					.join('\u0000');
			})()`, &raw))

		parts := strings.Split(raw, "\u0000")

		return parts[0], parts[1], parts[2]
	}

	typeDate := func(text string) {
		p.run("type "+text, chromedp.Evaluate(
			`document.querySelector('#form-timesheet .date-shown').value = ''`, nil),
			chromedp.SendKeys(`#form-timesheet .date-shown`, text, chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond))
	}

	// The suite pins the browser to en-US and this account has chosen nothing, so
	// this is the American order - and the placeholder says which order it is,
	// rather than leaving somebody to find out by being refused.
	_, _, hint := read()
	if hint != "MM/DD/YYYY" {
		t.Errorf("the field asks for %q, not the order this locale writes", hint)
	}

	// Typed in that order, and understood.
	typeDate("12/25/2026")

	if shown, iso, _ := read(); iso != "2026-12-25" {
		t.Errorf("typing 12/25/2026 stored %q and shows %q", iso, shown)
	}

	// Now German. The same day, written the other way - and the value underneath
	// is untouched, because the value was never the thing that differed.
	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText("#tabs", "Zeiteinträge")

	p.run("back to the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	shown, iso, hint := read()

	if hint != "TT.MM.JJJJ" {
		t.Errorf("the German field asks for %q", hint)
	}

	if shown != "25.12.2026" {
		t.Errorf("the German field shows %q, want 25.12.2026", shown)
	}

	if iso != "2026-12-25" {
		t.Errorf("switching the language changed the stored value to %q", iso)
	}

	// And typing in the German order.
	typeDate("24.12.2026")

	if _, iso, _ := read(); iso != "2026-12-24" {
		t.Errorf("typing 24.12.2026 stored %q", iso)
	}

	// Half a date is not a date. The stored value stays as it was rather than
	// becoming a guess at what was meant.
	typeDate("24.12")

	if _, iso, _ := read(); iso != "2026-12-24" {
		t.Errorf("a half-typed date changed the stored value to %q", iso)
	}

	// Nor is a day that does not exist.
	typeDate("30.02.2026")

	if _, iso, _ := read(); iso == "2026-03-02" {
		t.Error("the thirtieth of February was rolled forward into March")
	}
}

// And it is usable on the things people actually hold.
//
// The native field being kept is most of the answer - a phone opens its own date
// wheel - but the field beside it still has to be reachable with a thumb and has
// to fit. 44 by 44 is what a touch guideline asks for, and 32 by 30 is what suits
// a mouse; the rule is on the pointer rather than on the width, because a small
// window on a desktop is still a mouse.
func TestADateFieldIsUsableOnAPhone(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	for _, size := range []struct {
		name          string
		width, height int64
		touch         bool
		wantAtLeast   float64
	}{
		{"phone", 390, 844, true, 44},
		{"tablet", 820, 1180, true, 44},
		{"desktop", 1440, 900, false, 28},
	} {
		t.Run(size.name, func(t *testing.T) {
			if size.touch {
				p.run("resize", chromedp.Emulate(device.Info{
					Name: size.name, Width: size.width, Height: size.height,
					Scale: 2, Mobile: true, Touch: true,
				}), chromedp.Sleep(400*time.Millisecond))
			} else {
				p.run("resize", chromedp.EmulateViewport(size.width, size.height),
					chromedp.Sleep(400*time.Millisecond))
			}

			var box struct {
				ButtonW  float64 `json:"buttonW"`
				ButtonH  float64 `json:"buttonH"`
				Overlap  bool    `json:"overlap"`
				Overflow bool    `json:"overflow"`
			}

			p.run("measure", chromedp.Evaluate(`
				(() => {
					const w = document.querySelector('#form-timesheet .date-wrap');
					const shown = w.querySelector('.date-shown');
					const f = shown.getBoundingClientRect();
					const b = w.querySelector('.date-open').getBoundingClientRect();

					return {
						buttonW: b.width, buttonH: b.height,
						// The text has to stop before the button starts.
						overlap: b.left < f.right - parseFloat(
							getComputedStyle(shown).paddingRight),
						overflow: document.documentElement.scrollWidth > window.innerWidth + 1,
					};
				})()`, &box))

			if box.ButtonW < size.wantAtLeast || box.ButtonH < size.wantAtLeast {
				t.Errorf("the picker button is %.0fx%.0f, want at least %.0f square",
					box.ButtonW, box.ButtonH, size.wantAtLeast)
			}

			if box.Overlap {
				t.Error("the button sits over the text rather than beside it")
			}

			if box.Overflow {
				t.Error("the page scrolls sideways at this width")
			}
		})
	}
}
