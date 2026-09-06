//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// Nothing marked as a secret is still on screen when the session ends.
//
// The markup names them: three elements carry class="secret" - the enrolment's
// shared key, the otpauth URI that carries it with the account's address, and a
// token's value. Each was found separately, and each was left behind by a
// sign-out for its own reason.
//
// This checks the class rather than the three ids, so a fourth is covered by
// being marked rather than by somebody remembering this case exists. That is the
// whole reason the class is worth having: it is the markup saying which text on
// the page must not be handed on.
func TestNothingMarkedSecretSurvivesTheSignOut(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("start an enrolment and make a token",
		p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#totp-card", chromedp.ByID),
		p.click("#totp-begin"),
		chromedp.WaitVisible("#totp-setup", chromedp.ByID),
		chromedp.SendKeys(`#form-token input[name="name"]`, "Reader", chromedp.ByQuery),
		p.click(`#form-token button[type="submit"]`),
		chromedp.WaitVisible("#token-secret", chromedp.ByID))

	filled := func(when string) map[string]string {
		t.Helper()

		var out string

		p.run("read every secret "+when, chromedp.Evaluate(`JSON.stringify(
			Object.fromEntries([...document.querySelectorAll('.secret')]
				.map((node) => [node.id || node.className, node.textContent.trim()])))`,
			&out))

		var read map[string]string

		if err := json.Unmarshal([]byte(out), &read); err != nil {
			t.Fatalf("reading the secrets (%q): %v", out, err)
		}

		return read
	}

	before := filled("while signed in")

	if len(before) == 0 {
		t.Fatal("nothing on the page is marked as a secret, so this case checks nothing")
	}

	held := 0

	for _, value := range before {
		if value != "" {
			held++
		}
	}

	if held < 3 {
		t.Fatalf("only %d of %d marked elements are holding anything, so this case "+
			"is not covering what it means to: %v", held, len(before), before)
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	for id, value := range filled("after signing out") {
		if value == "" {
			continue
		}

		t.Errorf("#%s is marked as a secret and still holds %q after the session "+
			"ended", id, value)
	}
}
