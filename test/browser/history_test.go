//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A corrected entry offers its history, which shows what it said before the
// correction; an entry nobody corrected offers none, and the dialog goes with the
// Escape key and with a sign-out.
func TestACorrectedEntryShowsWhatItSaidBefore(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`(async () => {
		const corrected = await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 2 }),
		});
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 1.25 }),
		});

		// A second later, so the correction lands in another second than the booking:
		// the moments are kept to the second.
		await new Promise((resolve) => setTimeout(resolve, 1100));
		await api('/timesheets/' + corrected.id, {
			method: 'PUT', body: JSON.stringify({ durationHours: 3 }),
		});
		await reloadTimeViews();
		return true;
	})()`, nil)

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#table-timesheets", chromedp.ByID))

	p.waitForText("#table-timesheets tbody", "3.00")

	var offered int

	p.run("count the histories offered", chromedp.Evaluate(
		`document.querySelectorAll('#table-timesheets tbody button[data-action="history"]').length`, &offered))

	if offered != 1 {
		t.Errorf("%d entries offer a history, want only the corrected one", offered)
	}

	p.run("open the history", p.click(`#table-timesheets tbody button[data-action="history"]`),
		chromedp.WaitVisible(".history-card", chromedp.ByQuery))

	if shown := p.text(".history-card"); !strings.Contains(shown, "2.00 h") {
		t.Errorf("the history reads %q, want the two hours the entry said before", shown)
	}

	p.run("close it", chromedp.KeyEvent(kb.Escape))
	p.waitGone(".history-overlay")

	p.run("open it again", p.click(`#table-timesheets tbody button[data-action="history"]`),
		chromedp.WaitVisible(".history-card", chromedp.ByQuery))

	p.run("sign out", chromedp.Evaluate(`doLogout()`, nil),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left int

	p.run("look for what is left", chromedp.Evaluate(
		`document.querySelectorAll('.history-overlay').length`, &left))

	if left != 0 {
		t.Error("the history of the last account's entry stays in the page after the sign-out")
	}
}
