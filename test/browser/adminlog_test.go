//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// An account made by the administrator stands in the log on the Settings screen
// as a sentence naming it, under the administrator who made it - in English, and
// in German once the screen is.
func TestTheSettingsScreenShowsWhoChangedWhat(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "erika@example.com", "erika-password-1", "user")

	// Made past the interface, which reloads what a save of its own changed and
	// nothing else; the log is read again as the account screen's save would.
	var read bool

	p.awaitJSON(`(async () => { await loadChanges(); return true; })()`, &read)

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#changes-card", chromedp.ByID))

	p.waitForText("#table-changes tbody", "Created the account Erika")

	row := p.text("#table-changes tbody tr:first-child")
	if !strings.Contains(row, "Created the account Erika") {
		t.Errorf("the newest change reads %q, want the account Erika created", row)
	}

	p.chooseLanguage("de")
	p.waitForText("#table-changes tbody", "Konto Erika angelegt")
}
