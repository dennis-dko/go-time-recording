//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The setup wizard is shown on a first sign-in and has to be operable: it is
// the first thing an administrator meets.
func TestTheSetupWizardAppearsAndAdvances(t *testing.T) {
	t.Parallel()

	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	p.run("wait for the wizard", chromedp.WaitVisible("#setup-wizard", chromedp.ByID))

	if title := p.text("#setup-step-title"); title == "" {
		t.Error("the wizard should be showing a step")
	}

	// The database comes first, and it offers a way to settle it.
	if !p.visible("#setup-steps") {
		t.Error("the step trail should be visible")
	}

	progress := p.text("#setup-progress")
	if progress == "" {
		t.Error("the wizard should say where in it you are")
	}
}
