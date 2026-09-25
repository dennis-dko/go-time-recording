//go:build browser

package browser

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// The card greets by the installation's own name.
//
// Its heading said "Sign in", which is what the button under it says too, and
// named nothing - an installation that has been given a title had it in the tab
// and nowhere on the screen in front of whoever was about to sign in to it.
func TestTheSignInCardWelcomesByTheInstallationsName(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Titled "Zeiterfassung", with no logo, so the lettered mark stands in the
	// band beside the words.
	p.storeBranding(t, "")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))
	p.run("reload the sign-in screen", chromedp.Reload(),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.waitEvaluates("the heading of the sign-in card",
		`document.querySelector('#login-screen h2')?.textContent.trim() ?? ''`,
		"Welcome to Zeiterfassung")

	// The mark stands before the words, in the same band.
	var placed struct {
		MarkRight   float64 `json:"markRight"`
		HeadingLeft float64 `json:"headingLeft"`
		SameBand    bool    `json:"sameBand"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const mark = document.querySelector('#login-mark');
		const heading = document.querySelector('#login-screen h2');
		const band = heading.closest('.login-band');

		return {
			markRight: mark.getBoundingClientRect().right,
			headingLeft: heading.getBoundingClientRect().left,
			sameBand: band !== null && band.contains(mark),
		};
	})())`, &placed)

	if !placed.SameBand {
		t.Error("the mark and the heading are not in one band across the top of the card")
	}

	if placed.MarkRight > placed.HeadingLeft {
		t.Errorf("the mark ends at %.0fpx and the heading starts at %.0fpx, so the "+
			"words come before the picture", placed.MarkRight, placed.HeadingLeft)
	}
}

// The words stand beside the fields on a wide window and above them on a narrow
// one.
//
// Beside, because a sign-in form of two fields reads as one block that way and
// the card has the width for it. Above on a telephone, because a column of
// labels takes a third of a 390px screen away from the field somebody is typing
// into.
//
// Measured on the words themselves rather than on the label, which wraps the
// field and is therefore always exactly as wide as the row.
func TestTheSignInLabelsStandBesideTheFieldsAndAboveThemWhenNarrow(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.run("wait for the sign-in form", chromedp.WaitVisible("#form-login", chromedp.ByID))

	type placement struct {
		WordsRight  float64 `json:"wordsRight"`
		WordsMiddle float64 `json:"wordsMiddle"`
		WordsBottom float64 `json:"wordsBottom"`
		FieldLeft   float64 `json:"fieldLeft"`
		FieldTop    float64 `json:"fieldTop"`
		FieldBottom float64 `json:"fieldBottom"`
	}

	measure := func(name string) placement {
		var out placement

		p.evalJSON(fmt.Sprintf(`JSON.stringify((() => {
			const field = document.querySelector('#form-login input[name=%q]');
			const label = field.closest('label');
			const words = document.createRange();
			words.selectNodeContents(label.firstChild);

			const w = words.getBoundingClientRect();
			const f = field.getBoundingClientRect();

			return {
				wordsRight: w.right, wordsMiddle: w.top + w.height / 2, wordsBottom: w.bottom,
				fieldLeft: f.left, fieldTop: f.top, fieldBottom: f.bottom,
			};
		})())`, name), &out)

		return out
	}

	for _, name := range []string{"email", "password"} {
		wide := measure(name)

		if wide.WordsRight > wide.FieldLeft {
			t.Errorf("%s: the words end at %.0fpx and the field starts at %.0fpx, so "+
				"they are not beside it on a wide window", name, wide.WordsRight, wide.FieldLeft)
		}

		if wide.WordsMiddle < wide.FieldTop || wide.WordsMiddle > wide.FieldBottom {
			t.Errorf("%s: the words sit at %.0fpx, outside the field's %.0f-%.0fpx, so "+
				"they are not level with it", name, wide.WordsMiddle, wide.FieldTop, wide.FieldBottom)
		}
	}

	p.run("a telephone", chromedp.EmulateViewport(390, 844))

	for _, name := range []string{"email", "password"} {
		narrow := measure(name)

		if narrow.WordsBottom > narrow.FieldTop {
			t.Errorf("%s: the words end at %.0fpx and the field starts at %.0fpx, so "+
				"they are not above it on a narrow window", name, narrow.WordsBottom, narrow.FieldTop)
		}
	}
}
