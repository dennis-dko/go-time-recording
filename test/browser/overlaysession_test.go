//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Nothing is left covering the sign-in screen when the session ends.
//
// There are three full-screen overlays - the crop editor, the restart wait and
// the setup wizard - and .overlay is position: fixed, inset: 0, z-index 25. Each
// takes every press within it and hides everything under it.
//
// handBackTheScreen puts the sign-in screen up and takes down what belonged to
// the session that ended: the banners, the drafts, the forms, the pollers. It did
// not take down an overlay, and each overlay is hidden by its own flow instead -
// the wizard by loadSetup, which runs from refreshAll and so does not run on the
// way out.
//
// The wizard is the reachable one, and it is the worst of the three to be stuck
// behind: its dismiss button asks the server to complete the setup, which a
// session that has ended cannot do, so the answer is an error inside the overlay
// and the sign-in screen stays out of reach until the page is loaded again.
//
// Driven at the wizard because it comes up by itself on a first sign-in. The fix
// is not per-overlay, so this stands for all three.
func TestNoOverlayIsLeftOverTheSignInScreen(t *testing.T) {
	t.Parallel()

	p := open(t)

	// Deliberately not readyAdmin: that settles the wizard, and the wizard being
	// up is the state under test.
	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")
	p.settled()

	p.run("the wizard is up", chromedp.WaitVisible(`#setup-wizard`, chromedp.ByID))

	p.run("the session ends underneath it", chromedp.Evaluate(
		`handBackTheScreen('')`, nil))

	var covering []string

	p.run("what is still up", chromedp.Evaluate(`
		[...document.querySelectorAll('.overlay')]
			.filter((o) => !o.hidden)
			.map((o) => o.id || o.className)`, &covering))

	if len(covering) > 0 {
		t.Errorf("after the session ended these overlays are still up: %v. Each is "+
			"fixed over the whole viewport at z-index 25, and each is taken down by "+
			"its own flow rather than on the way out", covering)
	}
}
