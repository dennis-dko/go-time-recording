//go:build browser

package browser

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// The entries screen shows a page and offers the rest, rather than quietly
// stopping at the first hundred.
//
// Only a browser can check this. The screen used to ask for every entry the
// account had ever booked and render all of them, so bounding the endpoint would
// have changed what the table contains without changing a line of markup: the
// hundred-and-first entry would simply not be there, the table would look exactly
// as it always had, and nothing on the page would say the list was cut short.
// That is the failure this case exists for - a truncation that looks like a
// complete answer.
//
// The entries are seeded through the page's own api() rather than the form,
// because a round trip through the booking form per entry is a minute of clicking
// to set up one assertion, and the form is covered elsewhere. One entry per day so
// the daily ceiling is never the thing being tested.

// seedEntries books count entries on consecutive days, through the page.
func seedEntries(t *testing.T, p *page, count int) {
	t.Helper()

	p.run("seed entries", chromedp.Evaluate(`
		window.__seeded = 0;
		window.__seedMs = 0;
		(async () => {
			const began = Date.now();
			const day = new Date(Date.UTC(2026, 0, 1));
			for (let i = 0; i < `+strconv.Itoa(count)+`; i++) {
				const on = new Date(day.getTime() + i * 86400000);
				await api('/timesheets', { method: 'POST', body: JSON.stringify({
					date: on.toISOString().slice(0, 10),
					durationHours: 1,
					description: 'seeded ' + i,
				}) });
			}
			window.__seedMs = Date.now() - began;
			window.__seeded = 1;
		})();
		'started'`, nil))

	p.waitEvaluates("the entries are seeded", `String(window.__seeded ?? 0)`, "1")

	var took int

	p.evalJSON(`JSON.stringify(window.__seedMs)`, &took)
	t.Logf("seeded %d entries in %dms", count, took)
}

// showTimeView opens the time view and makes the table read the seeded entries.
//
// The loader is called rather than a tab clicked, because clicking the tab of the
// view already showing does nothing at all - which is what the first version of
// this case tripped over, and the symptom was an empty table rather than anything
// about paging. Setup only: every assertion below is about the rendered table and
// about a real click on a real button.
func showTimeView(p *page) {
	p.t.Helper()

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.run("read the seeded entries", chromedp.Evaluate(`loadTimesheets(); 'reloading'`, nil))
}

func TestTheEntriesScreenPagesInsteadOfTruncating(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// One more than a page, so there is exactly one row that a truncating screen
	// would lose and a paging one would offer.
	const seeded = 26

	// Seeded before the view is opened, so the table's first render is already the
	// paged one. Switching away and back to force a reload was the obvious
	// alternative and a worse test: it makes the case depend on two views settling
	// in order, and the failure when they do not is a timeout that says nothing
	// about paging.
	seedEntries(t, p, seeded)
	showTimeView(p)

	p.waitShown("#ts-more-row")

	shown := p.count("#table-timesheets tbody tr")
	if shown == seeded {
		t.Fatalf("the table rendered all %d entries, so the screen is still asking "+
			"for everything the account has ever booked", seeded)
	}

	if shown == 0 {
		t.Fatal("the table is empty after seeding")
	}

	// The screen has to say the list is longer than what is on it. Without this a
	// bounded table and a complete one are the same picture.
	if summary := p.text("#ts-shown"); summary == "" {
		t.Error("nothing on the screen says how many entries there are altogether")
	}

	p.run("ask for the rest", p.click("#ts-more"))

	p.waitGone("#ts-more-row")

	if after := p.count("#table-timesheets tbody tr"); after != seeded {
		t.Errorf("after asking for more the table holds %d of %d entries", after, seeded)
	}
}

func TestNothingOffersMoreWhenThereIsNoMore(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	seedEntries(t, p, 3)
	showTimeView(p)

	p.waitForText("#table-timesheets tbody", "seeded 2")

	if p.visible("#ts-more-row") {
		t.Error("the screen offers to show more with three entries and nothing hidden")
	}
}
