//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A week of leave recorded from the calendar's card is its five working days:
// listed in the card, marked in the month, and taken back one day at a time.
//
// Seven days from the first of the month the case runs in, which always hold
// five weekdays and are always in the month the calendar opens on.
func TestLeaveIsRecordedAndMarkedInTheCalendar(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#form-absence", chromedp.ByID))

	p.run("fill the week in", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-absence');
		const month = todayISO().slice(0, 7);
		setDateField(form.elements.from, month + '-01');
		setDateField(form.elements.to, month + '-07');
		form.elements.kind.value = 'vacation';
	})()`, nil))

	p.run("record it", p.click(`#form-absence button[type="submit"]`))

	p.waitForText(".toast-note", "Days recorded: 5")
	p.waitForRows("#table-absences tbody tr", 5)
	p.waitFor("the month to mark the five days", func() bool {
		return p.count("#calendar-days .cal-away") == 5
	})

	if marked := p.text("#calendar-days .cal-day.away"); marked == "" {
		t.Error("a day away carries no word for why")
	}

	p.run("take one day back", p.click("#table-absences button.danger"),
		chromedp.WaitVisible(".confirm-card", chromedp.ByQuery),
		p.click(".confirm-card button.confirm-proceed"))

	p.waitForRows("#table-absences tbody tr", 4)
	p.waitFor("the month to mark the four days left", func() bool {
		return p.count("#calendar-days .cal-away") == 4
	})
}
