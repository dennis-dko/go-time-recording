//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// Signing out takes the count of the last account's entries off the screen.
//
// The tally reads "Showing 26 of 41 entries" above the list, and it is drawn from
// timesheetEntries and timesheetTotal - module state, like the calendar's month
// and the stopwatch's interval. forgetTheLastAccount empties the tables and the
// filled lists, and its own comment gives the rule: "a table whose loader returns
// early keeps whatever it last held."
//
// The tally is not a table. It is a div beside the list, so nothing emptied it,
// and it stood over an emptied table saying how much the previous account had
// recorded. A loader replaces it - but every loader here returns at its first
// line when the right is absent, so for an account without timesheets:read:own it
// is never replaced at all.
//
// How much somebody recorded is their business: this is the same reasoning that
// hides the project record, where "opening a report was enough to learn what
// somebody had recorded against their own private category, and how much".
func TestSigningOutClearsTheEntryTally(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// One more than a page, which is what makes the tally appear at all.
	seedEntries(t, p, 26)
	showTimeView(p)

	p.waitShown("#ts-more-row")

	var counted string

	p.run("read the tally", chromedp.Evaluate(
		`document.querySelector('#ts-shown').textContent.trim()`, &counted))

	if counted == "" {
		t.Fatal("the tally is not showing, so this case would pass whatever the " +
			"sign-out does")
	}

	p.run("sign out", p.click("#logout"),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var after struct {
		Text   string `json:"text"`
		Offers bool   `json:"offers"`
	}

	var out string

	p.run("read it after signing out", chromedp.Evaluate(`(() => {
		const row = document.querySelector('#ts-more-row');

		return JSON.stringify({
			text: document.querySelector('#ts-shown').textContent.trim(),
			offers: row ? !row.hidden : false,
		});
	})()`, &out))

	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("reading the tally (%q): %v", out, err)
	}

	if after.Text != "" {
		t.Errorf("after signing out the screen still says %q, which counts the "+
			"previous account's entries", after.Text)
	}

	if after.Offers {
		t.Error("after signing out the list still offers to load more of the " +
			"previous account's entries")
	}
}
