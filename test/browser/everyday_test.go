//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A day chosen under "My account" makes every working day from it count, and
// both screens that add days up say so: the week grid's balance row has the
// Tuesday nobody booked on eight hours short, and the overtime table counts all
// five working days and names the rule it counted them by.
func TestEveryWorkingDayCountsOnceADayIsChosen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-working-times", chromedp.ByID))

	p.run("choose the day", chromedp.Evaluate(`setDateField(
		document.querySelector('#form-working-times').elements.everyDayCountsFrom, '2026-08-03')`, nil))
	p.run("save it", p.click(`#form-working-times button[type="submit"]`))

	p.waitFor("the day to be saved", func() bool {
		var saved string

		p.awaitJSON(`api('/me').then((me) => me.user.everyDayCountsFrom ?? '')`, &saved)

		return saved == "2026-08-03"
	})

	// The Monday booked, the rest of the week not.
	p.awaitJSON(`api('/timesheets', {
		method: 'POST', body: JSON.stringify({ date: '2026-08-03', durationHours: 8 }),
	}).then(() => true)`, nil)

	p.run("open that week", p.click(`.tab[data-view="week"]`),
		chromedp.WaitVisible("#table-week", chromedp.ByID),
		chromedp.Evaluate(`weekShown = '2026-08-03'; void loadWeekGrid()`, nil))

	p.waitForText("#table-week tfoot tr:last-child", "−8")

	p.run("work out that week's balance", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#form-overtime", chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const form = document.querySelector('#form-overtime');
			setDateField(form.elements.from, '2026-08-03');
			setDateField(form.elements.to, '2026-08-09');
		})()`, nil),
		p.click(`#form-overtime button[type="submit"]`))

	p.waitShown("#overtime-result")
	p.waitForRows("#table-overtime tbody tr", 5)
	p.waitForText("#overtime-rule", "Every working day counts from")
}
