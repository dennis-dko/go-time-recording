//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The calendar shows the month its arrows are on, whichever answer arrives last.
//
// Each press of an arrow moves the month and asks for it, and the grid was drawn
// from whichever answer came back last. Two quick presses ask for two months at
// once, and the one with more entries in it takes longer: when that was the
// first, the grid went back to it under arrows already on the second - and the
// next press went to the month after the second, skipping the one in between.
// The first month's answer is held back here so the order is the one tested,
// not one the machine may or may not produce.
func TestTheCalendarShowsTheMonthItsArrowsAreOn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID))
	p.atRest()

	var want string

	p.run("hold back the next month and press next twice", chromedp.Evaluate(`(() => {
		const from = currentCalendarMonth();
		const slow = new Date(from.getFullYear(), from.getMonth() + 1, 1);
		const last = new Date(from.getFullYear(), from.getMonth() + 2, 1);

		const real = window.fetch;
		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('from=' + ISO_DAY(slow))) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
			}

			return real(input, init);
		};

		const next = document.querySelector('#calendar-next');
		next.click();
		next.click();

		const names = 'January,February,March,April,May,June,July,August,September,October,November,December'.split(',');

		return names[last.getMonth()] + ' ' + last.getFullYear();
	})()`, &want))

	p.atRest()

	if got := p.text("#calendar-title"); got != want {
		t.Errorf("the arrows are on %s and the calendar shows %s", want, got)
	}
}
