//go:build browser

package browser

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// Entries recorded without a project are collected behind the link above the
// list, and given a project together from the selection bar - each through its
// own transfer, so the entry booked on a project already is left where it is.
func TestEntriesWithoutAProjectAreAssignedTogether(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var projectID int

	p.awaitJSON(`(async () => {
		const made = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Portal', startDate: '2026-01-01' }),
		});
		for (const hours of [1, 2, 3]) {
			await api('/timesheets', {
				method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: hours }),
			});
		}
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 4, projectId: made.id }),
		});
		await loadProjects();
		return made.id;
	})()`, &projectID)

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#table-timesheets", chromedp.ByID))

	// Loaded again here rather than before opening, so the count is drawn from
	// what the server now holds.
	p.run("load the list", chromedp.Evaluate(`void mutate(loadTimesheets, null, null)`, nil))
	p.waitForText("#ts-unassigned", "3")

	// The count beside the link is asked after the list is drawn, and on a slow
	// runner it came back after this case had looked: the link stood over the
	// list it leads to, once in CI and never here. Held back on purpose, so the
	// order that failed there is the order every run gets.
	p.run("hold the count back", chromedp.Evaluate(`(() => {
		const ask = window.fetch;
		window.fetch = (input, init) => (String(input).includes('limit=1')
			? new Promise((resolve) => setTimeout(resolve, 1500)).then(() => ask(input, init))
			: ask(input, init));
	})()`, nil))

	p.run("open the waiting ones", p.click("#ts-unassigned"))
	p.waitForRows("#table-timesheets tbody tr", 3)

	if p.visible("#ts-unassigned") {
		t.Error("the link is still offered over the list it leads to")
	}

	bar := `.bulk-bar[data-for="table-timesheets"]`

	p.run("tick them all", p.click("#table-timesheets thead .pick-all"))
	p.waitShown(bar + " .bulk-assign")

	// Four things in one row now, which a telephone does not have the width for.
	// Squeezed into one line, the project's list shrank to its arrow and both
	// buttons broke their words in two - all of it inside the screen, so a check
	// for overflow passed it. What is asked is that nothing in the bar is narrower
	// than it is on its own: the bar folds instead.
	var squeezed []string

	p.run("look at it on a telephone", chromedp.EmulateViewport(360, 740))
	p.awaitJSON(`(() => {
		const parts = [...document.querySelector('`+bar+`').children];
		const actual = parts.map((part) => part.getBoundingClientRect().width);
		for (const part of parts) part.style.flex = 'none';
		const natural = parts.map((part) => part.getBoundingClientRect().width);
		for (const part of parts) part.style.flex = '';
		return parts
			.map((part, i) => (actual[i] + 1 < natural[i]
				? part.className + ' is ' + Math.round(actual[i]) + 'px of ' + Math.round(natural[i]) + 'px'
				: ''))
			.filter(Boolean);
	})()`, &squeezed)

	for _, part := range squeezed {
		t.Errorf("on a 360px screen the selection bar squeezes %s", part)
	}

	if page := p.pixels("html", "scrollWidth"); page > 360 {
		t.Errorf("on a 360px screen the page is %dpx wide", page)
	}

	p.run("back to a desktop", chromedp.EmulateViewport(1600, 900))

	p.run("choose the project", chromedp.Evaluate(`document.querySelector('`+bar+` .bulk-project').value = '`+
		strconv.Itoa(projectID)+`'`, nil))
	p.run("assign them", p.click(bar+" .bulk-assign"))

	p.waitFor("no entry to be waiting for a project", func() bool {
		var waiting int

		p.awaitJSON(`api('/timesheets?projectId=none').then((list) => list.totalCount)`, &waiting)

		return waiting == 0
	})

	var onThePortal int

	p.awaitJSON(`api('/timesheets?projectId=`+strconv.Itoa(projectID)+`').then((list) => list.totalCount)`, &onThePortal)

	if onThePortal != 4 {
		t.Errorf("the project holds %d entries, want the three assigned and the one booked on it", onThePortal)
	}

	// Waited for: the server holding the last transfer comes a moment before the
	// page has its answer and says so.
	p.waitForText(".toast-note", "3 assigned to Portal")
}
