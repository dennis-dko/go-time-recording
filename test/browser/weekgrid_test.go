//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// typeIntoCell replaces what a grid cell holds and leaves it, which is what
// commits it - the way a person tabs from one day to the next.
func (p *page) typeIntoCell(selector, value string) {
	p.t.Helper()

	p.run("type "+value+" into "+selector,
		chromedp.SetValue(selector, "", chromedp.ByQuery),
		chromedp.SendKeys(selector, value+kb.Tab, chromedp.ByQuery))
}

// entriesOn reads, from the server, the hours booked on a day.
func (p *page) entriesOn(day string) []float64 {
	p.t.Helper()

	var hours []float64

	p.awaitJSON(`api('/timesheets?from=`+day+`&to=`+day+`').then((list) =>
		list.items.map((entry) => entry.durationHours))`, &hours)

	return hours
}

// The week is booked on the grid: a row added for a project, a number typed into
// its Monday books it, a changed number corrects that entry, and a second entry on
// the same day turns the cell into a way to them rather than a field.
func TestTheWeekIsBookedOnTheGrid(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var projectID int

	p.awaitJSON(`(async () => {
		const made = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Portal', startDate: '2026-01-01' }),
		});
		await loadProjects();
		return made.id;
	})()`, &projectID)

	var monday string

	p.awaitJSON(`mondayOf(todayISO())`, &monday)

	p.run("open the week", p.click(`.tab[data-view="week"]`),
		chromedp.WaitVisible("#table-week", chromedp.ByID))

	p.waitForText("#week-title", strconv.Itoa(time.Now().Year()))

	// Picked and dispatched by hand rather than through chooseOption: choosing a
	// project adds its row and takes it out of the list at once, so the read-back
	// chromedp's SetValue makes finds the select empty and calls that a failure.
	p.waitForNode(`#week-add-project option[value="` + strconv.Itoa(projectID) + `"]`)
	p.run("add a row for the project", chromedp.Evaluate(`(() => {
		const select = document.querySelector('#week-add-project');
		select.value = '`+strconv.Itoa(projectID)+`';
		select.dispatchEvent(new Event('change', { bubbles: true }));
	})()`, nil))

	cell := `#table-week input[data-cell="` + strconv.Itoa(projectID) + `|` + monday + `"]`

	p.waitForNode(cell)
	p.typeIntoCell(cell, "2.5")

	p.waitFor("the Monday to be booked", func() bool {
		hours := p.entriesOn(monday)

		return len(hours) == 1 && hours[0] == 2.5
	})

	// A changed number corrects the one entry behind the cell rather than adding
	// a second.
	p.waitForValue(cell)
	p.typeIntoCell(cell, "3")

	p.waitFor("the Monday to be corrected", func() bool {
		hours := p.entriesOn(monday)

		return len(hours) == 1 && hours[0] == 3
	})

	if booked := p.text("#table-week tfoot tr:first-child"); !strings.Contains(booked, "3") {
		t.Errorf("the booked row reads %q, want the three hours", booked)
	}

	// A second entry on the same day: the cell can no longer say which of the two
	// a new number would mean, so it becomes the way to them.
	p.awaitJSON(`(async () => {
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ projectId: `+strconv.Itoa(projectID)+`, date: '`+monday+`', durationHours: 1 }),
		});
		await loadWeekGrid();
		return true;
	})()`, nil)

	p.waitForNode("#table-week .week-several")

	if p.count(cell) != 0 {
		t.Error("a cell with two entries behind it is still a field")
	}
}

// waitFor polls a condition until it holds or the patience runs out.
func (p *page) waitFor(what string, holds func() bool) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if holds() {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("waited for %s and it never came; the log says:\n%s", what, p.app.Log())
}
