//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// An administrator who is not offered the directory run is told where it is.
//
// The card is taken away from an account that administers and also records
// time: a run deletes accounts with the hours on them, so it belongs to an
// account with no working day of its own. Taken away, and nothing put in its
// place - the Settings screen simply had one card fewer - while the tour went
// on describing the reconciliation "below" the directory card. That sentence is
// shown to exactly these accounts, since one that only administers gets the
// setup wizard instead of the tour. A card that is absent cannot be told from a
// feature that has been removed, and the question that prompted this case was
// that one: had the synchronisation not been on the Settings screen before?
func TestAnAdministratorWhoAlsoWorksIsToldWhereTheDirectoryRunIs(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	openSettings := func() {
		t.Helper()

		p.run("open Settings", p.click(`.tab[data-view="admin"]`),
			chromedp.WaitVisible("#form-ldap", chromedp.ByID))

		// The last thing the screen writes, and never empty - see
		// TestAGrantedAdministratorIsNotOfferedTheDirectoryRun.
		p.waitForFilled("#sync-schedule-active")
	}

	tourSays := func() string {
		t.Helper()

		var said string

		p.run("read what the tour says about the directory", chromedp.Evaluate(
			`TOUR_STEPS.find((step) => step.target === '#form-ldap').text()`, &said))

		return said
	}

	// The account the run belongs to: the card, described as being below, and
	// no note beside it.
	openSettings()

	if !p.visible("#sync-card") || p.visible("#sync-elsewhere") {
		t.Fatalf("the built-in administrator sees the card: %v, and the note saying it is elsewhere: %v",
			p.visible("#sync-card"), p.visible("#sync-elsewhere"))
	}

	if said := tourSays(); !strings.Contains(said, "Below it") {
		t.Errorf("the tour no longer tells the account that has the card where it is: %q", said)
	}

	p.createAccount(t, "bothe@example.com", "both-jobs-password-1", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("bothe@example.com", "both-jobs-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	openSettings()

	for _, language := range []struct{ code, below string }{
		{"en", "Below it"},
		{"de", "Darunter"},
	} {
		p.chooseLanguage(language.code)

		var role string

		p.run("read what the administering role is called", chromedp.Evaluate(`roleTitle('admin')`, &role))

		if p.visible("#sync-card") {
			t.Fatalf("%s: the card is offered to an account the server refuses", language.code)
		}

		if !p.visible("#sync-elsewhere") {
			t.Fatalf("%s: the card is gone and nothing says where the directory run is", language.code)
		}

		note := p.text("#sync-elsewhere")

		if !strings.Contains(note, role) {
			t.Errorf("%s: the note does not say which role the run belongs to, %q: %q", language.code, role, note)
		}

		said := tourSays()

		if strings.Contains(said, language.below) {
			t.Errorf("%s: the tour tells an account without the card that the reconciliation is below: %q",
				language.code, said)
		}

		if !strings.Contains(said, role) {
			t.Errorf("%s: the tour does not say whose the reconciliation is, %q: %q", language.code, role, said)
		}
	}

	// Nothing in the note can be pressed: the server refuses this account the
	// run, and a sentence is the one thing here that cannot be a control.
	if controls := p.count("#sync-elsewhere button, #sync-elsewhere input, #sync-elsewhere a"); controls != 0 {
		t.Errorf("the note carries %d control(s)", controls)
	}
}
