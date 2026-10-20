//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A budget shows in the project list as the share of it booked, and the booking
// that takes a project past four fifths of it says so - and leaves the list
// marking it.
func TestABudgetIsShownAndPassingFourFifthsIsNoticed(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var projectID int

	p.awaitJSON(`(async () => {
		const made = await api('/projects', {
			method: 'POST', body: JSON.stringify({ name: 'Portal', startDate: '2026-01-01', budgetHours: 10 }),
		});
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 7, projectId: made.id }),
		});
		await loadProjects();
		return made.id;
	})()`, &projectID)

	p.run("open the projects", p.click(`.tab[data-view="projects"]`),
		chromedp.WaitVisible("#table-projects", chromedp.ByID))

	p.waitForText("#table-projects .budget-line", "70 %")

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	p.run("book one and a half hours on it", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-timesheet');
		form.elements.projectId.value = '`+strconv.Itoa(projectID)+`';
		form.elements.durationHours.value = '1.5';
	})()`, nil),
		p.click("#timesheet-submit"))

	p.waitForText("#toast", "four fifths of its budget")

	if line := p.text("#table-projects .budget-line.near"); !strings.Contains(line, "85 %") {
		t.Errorf("the list marks %q, want the project marked at 85 %%", line)
	}
}
