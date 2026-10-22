//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A project made billable at a rate in its form says so in the list, and the
// statement of work under Reports charges its hours at that rate - a line for the
// project and a total in its currency, in the reader's own way of writing money.
func TestAStatementOfWorkChargesABillableProjectAtItsRate(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open Projects", p.click(`.tab[data-view="projects"]`),
		chromedp.WaitVisible("#form-project", chromedp.ByID))
	p.run("make a billable project",
		chromedp.SendKeys(`#form-project input[name="name"]`, "Consulting", chromedp.ByQuery),
		p.click(`#form-project label.switch .switch-track`),
		chromedp.SetValue(`#form-project input[name="hourlyRate"]`, "85.5", chromedp.ByQuery),
		p.click("#project-submit"))
	p.waitForText("#table-projects tbody", "billable at €85.50 an hour")

	var booked bool

	p.awaitJSON(`(async () => {
		const project = cache.projects.find((p) => p.name === 'Consulting');
		await api('/timesheets', {
			method: 'POST',
			body: JSON.stringify({ projectId: project.id, date: todayISO(), durationHours: 2.5 }),
		});
		return true;
	})()`, &booked)

	p.run("work out the statement", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-statement", chromedp.ByID),
		p.click(`#form-statement button[type="submit"]`))
	p.waitForText("#table-statement tbody", "Consulting")

	if row := p.text("#table-statement tbody"); !strings.Contains(row, "€85.50") || !strings.Contains(row, "€213.75") {
		t.Errorf("the statement's line reads %q, want the rate €85.50 and €213.75 for 2.5 hours", row)
	}

	if foot := p.text("#table-statement tfoot"); !strings.Contains(foot, "Total in EUR") || !strings.Contains(foot, "€213.75") {
		t.Errorf("the statement's total reads %q, want €213.75 in EUR", foot)
	}

	p.chooseLanguage("de")
	p.waitForText("#table-statement tfoot", "Summe in EUR")

	// A German amount keeps its figure and its sign together with a space that
	// does not break, which is what the runtime writes and what the reader wants.
	if foot := p.text("#table-statement tfoot"); !strings.Contains(foot, "213,75\u00a0€") {
		t.Errorf("the German total reads %q, want the amount written 213,75 €", foot)
	}
}
