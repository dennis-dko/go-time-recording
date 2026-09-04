//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The wizard says why it did not go on.
//
// setupError exists for that, and advanceSetup uses it for the one thing that
// can fail on the way: the step's own submit. The two awaits after it are not
// covered - the re-read of /setup in nextSetupStep, and refreshAll in
// finishSetup - and advanceSetup is wired straight to the Next button, so a
// rejection there is unhandled. Nothing on this page catches one, so the wizard
// simply does not advance: no error, no notice, a button that appears to do
// nothing.
//
// This is the screen somebody meets before anything else works, and the step it
// stops on is the one that has just changed the administrator password - so the
// obvious response, pressing Next again, fails on the old password as well.
//
// Driven at the real first-run flow rather than at a mocked wizard: signed in
// with the documented password, on the step that is up because the server
// refuses everything else until it is settled. The password step takes both
// fields - the third argument to passwordField is the placeholder, not a value,
// so a case that fills only the new one never gets past submit and would pass
// against unfixed code on the refusal it caused itself.
func TestTheSetupWizardSaysWhyItDidNotAdvance(t *testing.T) {
	t.Parallel()

	p := open(t)

	// Deliberately not readyAdmin: that helper settles the wizard, which is the
	// screen under test here.
	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")
	p.settled()

	p.run("the wizard is up",
		chromedp.WaitVisible(`#setup-wizard`, chromedp.ByID),
		chromedp.WaitVisible(`#setup-step-fields input[name="newPassword"]`, chromedp.ByQuery))

	// The state re-read the Next button makes, refused. The password change it
	// makes first is left alone and its outcome recorded, so this case can say
	// whether it reached the await it is about instead of assuming it did.
	p.run("refuse the state re-read", chromedp.Evaluate(`(() => {
		const real = window.fetch;
		window.__passwordStatus = 0;

		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;
			const method = (init && init.method ? init.method : 'GET').toUpperCase();

			if (method === 'GET' && url.includes('/api/v1/setup')) {
				return new Response('{"error":{"code":"internal"}}', {
					status: 500,
					headers: { 'Content-Type': 'application/json' },
				});
			}

			const response = await real(input, init);

			if (url.includes('/api/v1/me/password')) window.__passwordStatus = response.status;

			return response;
		};

		return true;
	})()`, nil))

	p.run("settle the step and press Next",
		chromedp.SendKeys(`#setup-step-fields input[name="currentPassword"]`,
			harness.AdminPassword, chromedp.ByQuery),
		chromedp.SendKeys(`#setup-step-fields input[name="newPassword"]`,
			adminPassword, chromedp.ByQuery),
		p.click(`#setup-next`))

	time.Sleep(2 * time.Second)

	var changed int

	p.run("did the step itself take", chromedp.Evaluate(
		`window.__passwordStatus`, &changed))

	if changed != 200 {
		t.Fatalf("the password step did not succeed (status %d), so this case never "+
			"reached the re-read it is about and proves nothing either way", changed)
	}

	var said string

	p.run("what the wizard says", chromedp.Evaluate(`(() => {
		const box = document.querySelector('#setup-error');

		return box && !box.hidden ? box.textContent.trim() : '';
	})()`, &said))

	if strings.TrimSpace(said) == "" {
		t.Error("the administrator password was changed and then the wizard said " +
			"nothing at all. advanceSetup hangs straight off the button, and the " +
			"re-read of /setup inside nextSetupStep is outside the try that reports " +
			"a failed step, so the rejection goes unhandled and Next appears to have " +
			"done nothing - on the one step where it did the most")
	}

	t.Logf("the wizard said: %q", said)
}
