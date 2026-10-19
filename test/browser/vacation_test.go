//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The leave card says what is left of the year: the days given under it, less the
// days of leave recorded below it - and it follows a recording as it is made.
//
// The leave is the Monday and Tuesday of the first whole week of March of the year
// the case runs in: always working days, never a holiday without a state chosen,
// and always in the year the card opens on.
func TestTheLeaveCardSaysWhatIsLeftOfTheYear(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#form-vacation", chromedp.ByID))

	p.run("give thirty days a year", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-vacation');
		form.elements.days.value = '30';
		form.elements.fromYear.value = todayISO().slice(0, 4);
	})()`, nil),
		p.click(`#form-vacation button[type="submit"]`))

	p.waitForText("#vacation-left", "Days left: 30")

	p.run("record two days of leave", chromedp.Evaluate(`(() => {
		const year = Number(todayISO().slice(0, 4));
		const monday = new Date(Date.UTC(year, 2, 1));
		while (monday.getUTCDay() !== 1) monday.setUTCDate(monday.getUTCDate() + 1);
		const tuesday = new Date(monday.getTime() + 86400e3);
		const form = document.querySelector('#form-absence');
		setDateField(form.elements.from, monday.toISOString().slice(0, 10));
		setDateField(form.elements.to, tuesday.toISOString().slice(0, 10));
		form.elements.kind.value = 'vacation';
	})()`, nil),
		p.click(`#form-absence button[type="submit"]`))

	p.waitForText("#vacation-left", "Days left: 28")
	p.waitForText("#vacation-detail", "Given: 30")
}
