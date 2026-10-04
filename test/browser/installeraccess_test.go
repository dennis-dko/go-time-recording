//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Somebody working the installer by keyboard, or with a screen reader, is told
// what came of a press and keeps their place.
//
// The answer of "Test connection" and of "Save and start" lands in a note under
// the buttons, which was not a live region: a screen reader announced nothing,
// so the press had no visible result for the people who cannot see the note.
// And both buttons were disabled while the request was under way, which takes
// the focus off the one that was pressed - the next Tab started again at the top
// of the page.
func TestTheInstallerAnswersAKeyboardAndAScreenReader(t *testing.T) {
	t.Parallel()

	app := harness.StartUnconfigured(t)

	ctx, done := installerInGerman(t, app)
	defer done()

	var region, focused string

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#token').value = 'not-the-token'`, nil),
		chromedp.Focus("#test", chromedp.ByID),
		chromedp.KeyEvent(kb.Enter),
		chromedp.WaitVisible(`#note.bad, #note.good`, chromedp.ByQuery),
		// The region around the note, or the note itself, that a screen reader
		// watches - empty when there is none.
		chromedp.Evaluate(`(() => {
			const region = document.querySelector('#note')
				.closest('[aria-live], [role="status"], [role="alert"]');
			return region ? (region.getAttribute('aria-live') || region.getAttribute('role')) : '';
		})()`, &region),
		chromedp.Evaluate(`document.activeElement ? document.activeElement.id : ''`, &focused),
	); err != nil {
		t.Fatalf("pressing the test button from the keyboard: %v", err)
	}

	if region == "" {
		t.Error("the note the answer is written into is not a live region, so a screen " +
			"reader says nothing after the press")
	}

	if focused != "test" {
		t.Errorf("after the answer the focus is on %q rather than on the button that was "+
			"pressed, so a keyboard has lost its place", focused)
	}
}
