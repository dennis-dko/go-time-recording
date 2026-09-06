//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A paused log viewer goes on offering to resume after the language changes.
//
// The button says one of two things and carries the data-i18n it was written
// with, which names the running one - log.pause. applyLanguage translates every
// element from its key, so a language change while the viewer was paused put
// "Pause" back on a button whose only job is to say which state the viewer is in.
//
// The other three buttons of this shape - the wizard's Next and Skip, the tour's
// Next - are behind an overlay that covers the language picker, so the switch
// cannot be made while they are showing their other message. This one is on an
// ordinary screen with the picker in the bar above it, which is what makes it the
// one to drive; all four are fixed the same way.
//
// Checked by comparing the two states in the same language rather than against a
// German string written here: the assertion is that they differ, which is the
// whole job of the label.
func TestAPausedLogViewerStillSaysSoInAnotherLanguage(t *testing.T) {
	t.Parallel()

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open the log viewer", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	label := func(when string) string {
		t.Helper()

		var out string

		p.run("read the button "+when, chromedp.Evaluate(
			`document.querySelector('#log-pause').textContent.trim()`, &out))

		return out
	}

	running := label("while running")

	p.run("pause", p.click("#log-pause"))

	paused := label("while paused")

	if paused == running {
		t.Fatalf("the button reads %q both running and paused, so this case would "+
			"pass whatever a language change does", running)
	}

	p.chooseLanguage("de")

	pausedGerman := label("while paused, in German")

	p.run("resume", p.click("#log-pause"))

	runningGerman := label("while running, in German")

	if pausedGerman != runningGerman {
		return
	}

	t.Errorf("after the language change a paused viewer and a running one both "+
		"read %q. The button declares the running key, so applyLanguage put that "+
		"one back over the state the viewer was actually in", pausedGerman)
}
