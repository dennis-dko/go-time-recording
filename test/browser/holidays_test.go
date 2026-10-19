//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A state chosen under "My account" puts its holidays in the calendar, by name.
func TestAChosenStatesHolidaysAreMarkedInTheCalendar(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open my account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-my-holidays", chromedp.ByID))

	p.waitForNode(`#my-holidays option[value="BY"]`)

	// Chosen the way a person chooses: the change event the form listens for.
	p.run("choose Bavaria", chromedp.Evaluate(`(() => {
		const select = document.querySelector('#my-holidays');
		select.value = 'BY';
		select.dispatchEvent(new Event('change', { bubbles: true }));
	})()`, nil))
	p.run("save it", p.click(`#form-my-holidays button[type="submit"]`))

	p.waitFor("Bavaria to be in effect", func() bool {
		var region string

		p.awaitJSON(`api('/me/holidays').then((mine) => mine.effectiveRegion)`, &region)

		return region == "BY"
	})

	p.run("open December", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID),
		chromedp.Evaluate(`calendarMonth = monthOf('2026-12-01'); void loadCalendar()`, nil))

	p.waitForText("#calendar-days", "Christmas Day")
	p.waitForText("#calendar-days", "Boxing Day")
}
