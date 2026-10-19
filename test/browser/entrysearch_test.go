//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Searching the entry list from the screen: the text typed narrows the table once
// the typing pauses, and the range narrows it further.
func TestTheEntriesAreSearchedFromTheList(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`(async () => {
		for (const [date, description] of [
			['2026-08-03', 'Review of ticket ABC-12'],
			['2026-08-04', 'Lunch with the customer'],
			['2026-08-05', 'weekly review'],
		]) {
			await api('/timesheets', {
				method: 'POST', body: JSON.stringify({ date, durationHours: 1, description }),
			});
		}
		return true;
	})()`, nil)

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-ts-filters", chromedp.ByID),
		chromedp.SendKeys("#filter-ts-q", "review", chromedp.ByID))

	p.waitForRows("#table-timesheets tbody tr", 2)

	if text := p.text("#table-timesheets tbody"); strings.Contains(text, "Lunch") {
		t.Errorf("the search for review still shows the lunch: %q", text)
	}

	// One day of the two, as the picker sets it: the value, and a change that
	// bubbles - which a picked date's does, and setDateField's own does not.
	p.run("narrow it to one day", chromedp.Evaluate(`(() => {
		for (const id of ['#filter-ts-from', '#filter-ts-to']) {
			const field = document.querySelector(id);
			setDateField(field, '2026-08-05');
			field.dispatchEvent(new Event('change', { bubbles: true }));
		}
	})()`, nil))

	p.waitForRows("#table-timesheets tbody tr", 1)

	if text := p.text("#table-timesheets tbody"); !strings.Contains(text, "weekly review") {
		t.Errorf("one day of the search shows %q, want the weekly review", text)
	}
}

// waitForRows waits until exactly n elements match.
func (p *page) waitForRows(selector string, n int) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if p.count(selector) == n {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.t.Fatalf("%s matched %d time(s), never %d; the table reads %q",
		selector, p.count(selector), n, p.text(selector))
}
