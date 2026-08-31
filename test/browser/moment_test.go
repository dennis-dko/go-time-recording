//go:build browser

package browser

import (
	"regexp"
	"testing"

	"github.com/chromedp/chromedp"
)

// The token table shows a time of day, and shows it readably.
//
// Only a browser can check this half. The server now sends an RFC 3339 moment
// where it used to send a bare date, and fmtDate splits an ISO string on its
// hyphens - so "2026-08-31T14:03:22Z" made its third field NaN, the formatter gave
// up, and the cell rendered the raw timestamp. A column that reads
// 2026-08-31T14:03:22Z is not a fixed column; it is the same defect wearing the
// answer.
//
// The assertion is about shape rather than about a particular string: the
// rendering follows the reader's locale and their own zone, so pinning the exact
// text would pin the machine the suite happens to run on.

// aMomentOnScreen matches a rendered date followed by a time of day, in either
// order and in either of the two separators the locales here use.
var aMomentOnScreen = regexp.MustCompile(`\d{1,2}[./]\d{1,2}[./]\d{2,4}.*\d{1,2}:\d{2}`)

func TestATokenShowsWhenItWasMadeAndNotJustTheDay(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#token-card", chromedp.ByID))

	p.run("create a token",
		chromedp.SendKeys(`#form-token input[name="name"]`, "ci", chromedp.ByQuery),
		p.click(`#form-token button[type="submit"]`))

	p.waitForText("#table-tokens tbody", "ci")

	shown := p.text("#table-tokens tbody")

	if !aMomentOnScreen.MatchString(shown) {
		t.Errorf("the token row shows no time of day; the created column reads:\n%s", shown)
	}

	// The raw wire format reaching the screen is the other way this goes wrong, and
	// it looks like a working column until somebody reads one.
	if regexp.MustCompile(`\d{4}-\d{2}-\d{2}T`).MatchString(shown) {
		t.Errorf("the token row shows an unformatted timestamp:\n%s", shown)
	}
}
