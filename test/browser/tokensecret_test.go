//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A token's value does not outlive the screen it was shown on.
//
// The value exists once, in the response that created it, and the card says so
// where the reader can see it: "Copy it now — this value is never shown again."
// The code says the same thing about how long it stays: "the secret exists only
// in this response, so it is shown until the user navigates away rather than in a
// toast that disappears."
//
// Navigating away does nothing to it. It is set in one place and cleared in none,
// so it stays in the document for the life of the page - through every screen,
// and through a sign-out, which is where it matters: this is a bearer credential
// that carries the account's whole role and works until somebody revokes it. The
// enrolment panel two cards above it is cleared on both of those, and by
// comparison a half-finished second factor is the smaller thing to leave behind.
//
// Both halves are checked, because the comment claims one of them and the account
// needs the other.
func TestATokenValueDoesNotOutliveTheScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("create a token", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-token", chromedp.ByID),
		chromedp.SendKeys(`#form-token input[name="name"]`, "Reader", chromedp.ByQuery),
		p.click(`#form-token button[type="submit"]`),
		chromedp.WaitVisible("#token-secret", chromedp.ByID))

	read := func(when string) (value string, shown bool) {
		t.Helper()

		var out string

		p.run("read the panel "+when, chromedp.Evaluate(`JSON.stringify({
			value: document.querySelector('#token-secret-value').textContent.trim(),
			shown: !document.querySelector('#token-secret').hidden,
		})`, &out))

		var panel struct {
			Value string `json:"value"`
			Shown bool   `json:"shown"`
		}

		if err := json.Unmarshal([]byte(out), &panel); err != nil {
			t.Fatalf("reading the panel (%q): %v", out, err)
		}

		return panel.Value, panel.Shown
	}

	secret, shown := read("just after creating")

	if secret == "" || !shown {
		t.Fatalf("the token value is not on screen (value %q, shown %v), so this "+
			"case would pass whatever happens to it", secret, shown)
	}

	// The half the comment claims.
	p.run("go somewhere else", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	awayValue, awayShown := read("after navigating away")

	if awayValue != "" || awayShown {
		t.Errorf("the token value is still in the document after leaving the screen "+
			"(value %q, shown %v). It is set in one place and cleared in none",
			awayValue, awayShown)
	}

	// And the half that belongs to the account.
	p.run("back to the card", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-token", chromedp.ByID))

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	afterValue, afterShown := read("after signing out")

	if afterValue != "" || afterShown {
		t.Errorf("the token value is still in the document after signing out "+
			"(value %q, shown %v). It is a bearer credential carrying the whole of "+
			"that account's role, and it works until somebody revokes it",
			afterValue, afterShown)
	}
}
