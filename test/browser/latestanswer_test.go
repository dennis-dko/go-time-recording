//go:build browser

package browser

import (
	"strings"
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

// The time entries show the project the filter is on, whichever answer arrives
// last.
//
// The list was emptied when a load began and the answer added to it when it
// came back. Two loads at once - stepping through the filter with the arrow
// keys sends one per step - both emptied it first, and then both added: the list
// held the entries of two projects under a filter naming one, with the total of
// whichever answered last.
func TestTheTimeEntriesShowTheProjectTheFilterIsOn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.bookAnHourOn(t, "First")
	p.bookAnHourOn(t, "Second")

	// Until refreshAll says it has finished, not only until nothing is in flight:
	// the projects come several loaders in, and the strip is put away in any gap
	// longer than its fade - the page translating itself after /me on a busy
	// runner is one - so at rest alone let this read a cache without them.
	p.run("load the screen again", chromedp.Evaluate(`void refreshAll()`, nil))
	p.settled()
	p.atRest()

	p.run("hold back the first project and pick both in turn", chromedp.Evaluate(`(() => {
		const id = (name) => String(cache.projects.find((project) => project.name === name).id);
		const first = id('First');

		const real = window.fetch;
		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/timesheets?') && url.includes('projectId=' + first + '&')) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
			}

			return real(input, init);
		};

		const filter = document.querySelector('#filter-ts-project');

		for (const name of ['First', 'Second']) {
			filter.value = id(name);
			filter.dispatchEvent(new Event('change'));
		}

		return 1;
	})()`, nil))

	p.atRest()

	var shown []string

	p.run("read the list", chromedp.Evaluate(
		`[...document.querySelectorAll('#table-timesheets tbody tr')].map((row) => row.textContent)`, &shown))

	if len(shown) != 1 || !strings.Contains(shown[0], "Second") {
		t.Errorf("the filter is on Second and the list holds %d row(s): %q", len(shown), shown)
	}
}
