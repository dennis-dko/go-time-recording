//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// "Carry on where you were" has to land somewhere that exists.
//
// The greeting's Continue button goes to onwardView(), which answers with the
// screen this account was last on - read straight out of localStorage. That value
// is nothing more than a string that was true once, and startingView says so in
// its own comment, about the same value: "a remembered screen may belong to a tab
// this account has since lost, or one that stopped existing between releases".
// startingView checks it against the tabs. onwardView did not.
//
// A view that no longer exists therefore went through unchecked. switchView then
// hides every view and shows #view-<gone>, which is nothing - a blank page with no
// tab marked current, and no way out but the title or a reload. This project has
// removed a view before: the review path went, and there is a migration named
// after retiring it.
//
// A view the account has merely lost is the milder half, and it was already
// caught by viewTheReaderMayHave falling back to the greeting - so Continue did
// nothing at all, which is its own small wrongness.
//
// Seeded rather than provoked: reaching this state honestly means an upgrade
// across a release that removed a screen, and the state is one localStorage key.
func TestCarryingOnFromAViewThatNoLongerExistsLandsSomewhere(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The screen this account was last on, as a release that no longer has it
	// would have left it behind.
	p.run("remember a view that is gone", chromedp.Evaluate(
		`localStorage.setItem('gtr_view_' + me.user.id, 'reviews'); true`, nil))

	p.run("back to the greeting", p.click(`#app-title`),
		chromedp.WaitVisible("#view-welcome", chromedp.ByID))

	p.run("carry on", p.click(`#welcome-continue`))

	var visible string

	p.run("which view is on screen", chromedp.Evaluate(
		`(() => {
			const shown = [...document.querySelectorAll('.view')].filter((v) => !v.hidden);
			return shown.map((v) => v.id).join(',');
		})()`, &visible))

	if visible == "" {
		t.Fatal("carrying on from a view that no longer exists left every screen " +
			"hidden: a blank page, with no tab marked current and no way back but " +
			"the title or a reload")
	}

	t.Logf("landed on %q", visible)
}
