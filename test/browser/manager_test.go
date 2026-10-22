//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// teamPassword is what the two accounts of the manager case sign in with.
const teamPassword = "team-password-1"

// The manager's way through the screens, end to end: an administrator switches
// managers on from the Settings card and assigns somebody from the accounts
// table, the manager turns the screens to that account with the picker under the
// tabs and books for it - with the cards that are always one's own put away - and
// the person looked after reads who did, beside the entry and under My account.
func TestAManagerBooksForTheAccountAssignedToThemAndItSaysSo(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	var made struct {
		Mona uint `json:"mona"`
		Bea  uint `json:"bea"`
	}

	p.awaitJSON(`(async () => {
		const make = (name, email, role) => api('/users', {
			method: 'POST',
			body: JSON.stringify({ name, email, role, password: '`+teamPassword+`' }),
		});

		const mona = await make('Mona', 'mona@example.com', 'manager');
		const bea = await make('Bea', 'bea@example.com', 'user');

		await refreshAll();

		return { mona: mona.id, bea: bea.id };
	})()`, &made)

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-managers", chromedp.ByID))
	p.run("switch managers on", p.click(`#form-managers .switch-track`),
		p.click(`#form-managers button[type="submit"]`))
	p.run("confirm switching them on", p.click(".confirm-proceed"))

	// The accounts table offers the assignment once the installation has managers.
	p.run("open Users", p.click(`.tab[data-view="users"]`))
	p.waitForNode(`#table-users button[data-action="managers"]`)

	p.run("open Bea's managers", chromedp.Evaluate(`[...document.querySelectorAll('#table-users tbody tr')]
		.find((row) => row.textContent.includes('bea@example.com'))
		.querySelector('button[data-action="managers"]').click()`, nil))
	p.run("tick Mona", chromedp.WaitVisible(".choice-list", chromedp.ByQuery),
		chromedp.Evaluate(`[...document.querySelectorAll('.choice-list label')]
			.find((label) => label.textContent.includes('Mona')).querySelector('input').click()`, nil),
		p.click(".confirm-proceed"))

	var assigned []uint

	p.awaitJSON(`(async () => {
		for (let i = 0; i < 100; i += 1) {
			const answer = await api('/users/`+strconv.Itoa(int(made.Bea))+`/managers');
			if (answer.managerIds.length) return answer.managerIds;
			await new Promise((resolve) => setTimeout(resolve, 100));
		}
		return [];
	})()`, &assigned)

	if len(assigned) != 1 || assigned[0] != made.Mona {
		t.Fatalf("Bea's managers are %v after the dialog, want Mona (%d)", assigned, made.Mona)
	}

	p.signInAgainAs("mona@example.com")

	p.run("look after Bea", chromedp.WaitVisible("#viewing-for", chromedp.ByID),
		p.chooseOption("#viewing-for-picker", strconv.Itoa(int(made.Bea))),
		chromedp.Evaluate(`document.querySelector('#viewing-for-picker')
			.dispatchEvent(new Event('change'))`, nil))
	p.waitForText("#viewing-text", "the time of Bea")
	p.settled()

	if p.visible("#timer-card") {
		t.Error("the stopwatch, which is always one's own, is on screen while looking after Bea")
	}

	p.run("book for Bea", p.click(`.tab[data-view="timesheets"]`),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "2", chromedp.ByQuery),
		chromedp.SetValue(`#form-timesheet input[name="description"]`, "written up for Bea", chromedp.ByQuery),
		p.click("#timesheet-submit"))
	p.waitForText("#table-timesheets tbody", "booked by Mona")

	p.signInAgainAs("bea@example.com")

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`))
	p.waitForText("#table-timesheets tbody", "written up for Bea")

	if row := p.text("#table-timesheets tbody"); !strings.Contains(row, "booked by Mona") {
		t.Errorf("Bea reads her entry as %q, want it to say Mona booked it", row)
	}

	p.run("open My account", p.click(`.tab[data-view="settings"]`))
	p.waitForText("#managers-list", "Mona")

	p.chooseLanguage("de")
	p.run("back to the entries", p.click(`.tab[data-view="timesheets"]`))
	p.waitForText("#table-timesheets tbody", "gebucht von Mona")
}

// signInAgainAs signs out and in as another of the case's accounts, past the
// greeting a first sign-in brings.
func (p *page) signInAgainAs(email string) {
	p.t.Helper()

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(email, teamPassword)
	p.waitGone("#login-screen")
	p.settled()
	p.settleWelcome()
}

// The roles screen offers the team rights only once the installation has
// managers: until then they would reach nobody, and a switch that does nothing
// is worse than none.
func TestTheTeamRightsAreOfferedOnlyWhileTheInstallationHasManagers(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Roles", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible("#permission-list", chromedp.ByID))
	p.waitForNode(`#permission-list input[value="timesheets:read:own"]`)

	if n := p.count(`#permission-list input[value$=":team"]`); n != 0 {
		t.Errorf("with managers off the role editor offers %d team right(s)", n)
	}

	p.awaitJSON(`(async () => {
		await api('/settings/managers', { method: 'PUT', body: JSON.stringify({ enabled: true }) });
		await refreshAll();
		return true;
	})()`, nil)

	p.waitForNode(`#permission-list input[value="timesheets:read:team"]`)

	if n := p.count(`#permission-list input[value$=":team"]`); n != 3 {
		t.Errorf("with managers on the role editor offers %d team right(s), want 3", n)
	}
}
