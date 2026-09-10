//go:build browser

package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// chooseOption picks a value from a select, once that value is there to pick.
//
// A select is in the markup from the start and its options arrive with a
// loader, so there is a window in which the element exists and is empty.
// chromedp's SetValue inside that window fails with "could not set value on
// node", which reads as a broken control rather than a case that arrived early -
// and it does so rarely enough to look like a different bug each time.
func (p *page) chooseOption(selector, value string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(waitPatience)

		for {
			var ready bool

			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(
				`Boolean(document.querySelector(%q)?.querySelector('option[value=%q]'))`,
				selector, value), &ready)); err != nil {
				return err
			}

			if ready {
				return chromedp.Run(ctx, chromedp.SetValue(selector, value, chromedp.ByQuery))
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("%s never offered the option %q", selector, value)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// click scrolls an element clear of the sticky top bar, then clicks it.
//
// chromedp scrolls a target to the top of the viewport, which is where the top
// bar is - so the click lands on a navigation tab instead. Centring it first is
// what a person does by scrolling naturally.
func (p *page) click(selector string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.Run(ctx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(fmt.Sprintf(
				`document.querySelector(%q).scrollIntoView({block: 'center'})`, selector), nil),
			chromedp.Sleep(120*time.Millisecond),
			chromedp.Click(selector, chromedp.ByQuery),
		)
	})
}

// drag takes hold of the middle of an element, moves the pointer and lets go.
//
// Real input rather than a call to the page's own functions: what is being asked
// about here is whether a corner of the selection can be grabbed and pulled, and
// a test that sets the selection directly would pass with nothing wired to the
// corner at all.
//
// Moved in steps, because one jump from press to release is not a drag: the
// handler that follows the pointer only hears about positions it is told about.
func (p *page) drag(selector string, dx, dy float64) {
	p.t.Helper()

	var at struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}

	p.evalJSON(fmt.Sprintf(`JSON.stringify((() => {
		const r = document.querySelector(%q).getBoundingClientRect();

		return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
	})())`, selector), &at)

	moves := []chromedp.Action{
		chromedp.MouseEvent(input.MousePressed, at.X, at.Y,
			chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	}

	const steps = 6

	for step := 1; step <= steps; step++ {
		share := float64(step) / steps

		moves = append(moves, chromedp.MouseEvent(input.MouseMoved,
			at.X+dx*share, at.Y+dy*share, chromedp.ButtonType(input.Left)))
	}

	moves = append(moves, chromedp.MouseEvent(input.MouseReleased, at.X+dx, at.Y+dy,
		chromedp.ButtonType(input.Left), chromedp.ClickCount(1)))

	p.run("drag "+selector, moves...)
}

// chooseLanguage switches the interface language and waits for what that starts.
//
// Choosing a language saves it to the account, and the save is followed by a
// reload of every screen - which refills every form on its way past. A case that
// switches language and types straight afterwards is racing that reload: on a
// slow run the answer lands after the typing and puts the fields back the way
// the server has them, and the case then fails complaining about a value it set
// itself. That is what "„“ ist keine Datenbank" was, in a case that had
// just chosen postgres.
//
// So the switch is not finished when the labels change; it is finished when
// nothing is in flight any more.
func (p *page) chooseLanguage(code string) {
	p.t.Helper()

	p.run("switch the language to "+code, p.chooseOption("#language-picker", code))
	p.atRest()

	// Waited on the effect rather than on the requests. atRest says nothing is in
	// flight, and applying a language is not a request - the choice is saved,
	// refreshAll follows, and applyLanguage runs inside it. A case that read the
	// screen as soon as the traffic stopped could read it before the words
	// changed, and did: a form correcting an entry was still showing its German
	// labels one line after the switch back to English, on a loaded runner and
	// never here.
	//
	// documentElement.lang is what applyLanguage sets, so it is the same fact the
	// words are drawn from.
	deadline := time.Now().Add(waitPatience)

	var applied string

	for time.Now().Before(deadline) {
		p.run("wait for the page to be in "+code, chromedp.Evaluate(
			`document.documentElement.lang`, &applied))

		if applied == code {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	p.t.Fatalf("the page never came up in %q; it is in %q", code, applied)
}
