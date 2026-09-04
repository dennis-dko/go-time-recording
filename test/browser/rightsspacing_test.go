//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The reload button has a line of air above it.
//
// This banner is the only one with a control of its own. The restart notice and
// the release notice are prose with the way onward inside the sentence, so
// nothing sat under them; here a button sits directly beneath the words, and
// without space it reads as part of the sentence rather than as the thing to
// press.
//
// The stylesheet already has the rule and names the case: .row-apart is "a line's
// worth of air above, for the places where the control below is a different kind
// of thing from the one above it". A button under a sentence saying the screen
// has stopped being true is exactly that.
//
// Measured as geometry rather than asserted as a class, so the case says what a
// reader would see and stays true however the space is arranged.
func TestTheRightsNoticeGivesItsButtonRoom(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Raised the way the server raises it, down the open stream.
	p.run("the server says the rights moved", chromedp.Evaluate(`
		announcements.dispatchEvent(new MessageEvent('permissions', {
			data: JSON.stringify({ revision: 'something-else-entirely' }),
		}))`, nil))

	p.waitShown("#rights-banner")

	var gap int

	p.run("measure the air above the button", chromedp.Evaluate(`(() => {
		const words = document.querySelector('#rights-banner span');
		const button = document.querySelector('#rights-reload');

		return Math.round(
			button.getBoundingClientRect().top - words.getBoundingClientRect().bottom);
	})()`, &gap))

	t.Logf("the button sits %dpx below the sentence", gap)

	// A line, near enough. The rule is 18px; this asks for most of one so the
	// case is about there being air rather than about the exact figure.
	const aLine = 12

	if gap < aLine {
		t.Errorf("the reload button sits %dpx under the sentence, which reads as "+
			"part of it. .row-apart already exists for this and says so: a line's "+
			"worth of air above a control that is a different kind of thing from "+
			"the words above it", gap)
	}
}
