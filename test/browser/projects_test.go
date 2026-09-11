//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Hours that belong to no project are called the same thing everywhere.
//
// Three strings said it three ways - "ohne Projekt" in the tables and the
// stopwatch, "Ohne Projekt" in the dropdowns, "kein Projekt" in the charts -
// which reads as three different states rather than one.
func TestHoursWithoutAProjectAreNamedTheSameEverywhere(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	p.run("open the entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-project", chromedp.ByID))

	var said []string

	p.evalJSON(`JSON.stringify([
		document.querySelector('#timer-project option[value=""]')?.textContent ?? '',
		document.querySelector('#form-timesheet select[name="projectId"] option[value=""]')?.textContent ?? '',
	].filter(Boolean))`, &said)

	if len(said) < 2 {
		t.Fatalf("only %d of the two dropdowns offer the empty choice", len(said))
	}

	for _, wording := range said {
		if wording != said[0] {
			t.Errorf("the dropdowns say %q, so one screen names it differently", said)
		}
	}

	// And the one the reader was shown, which is the wording that was asked for.
	if !strings.Contains(said[0], "Kein Projekt") && !strings.Contains(said[0], "No project") {
		t.Errorf("the stopwatch offers %q for hours without a project", said[0])
	}
}

// A new project starts today, and ends when it ends.
//
// The date somebody would have to look up a calendar to type is filled in, the
// same courtesy the evaluation screens do with their period. The end is left
// empty on purpose: empty there means "no end planned", which is most projects,
// and filling it would invent a deadline.
func TestANewProjectStartsTodayAndHasNoEnd(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	p.run("open the projects", p.click(`.tab[data-view="projects"]`),
		chromedp.WaitVisible("#form-project", chromedp.ByID))

	start := p.value(`#form-project input[name="startDate"]`)
	end := p.value(`#form-project input[name="endDate"]`)

	if start == "" {
		t.Error("the start of a new project is empty, so it has to be typed in")
	}

	if end != "" {
		t.Errorf("the end of a new project is filled in with %q, which invents a "+
			"deadline nobody set", end)
	}

	// Today in the reader's own zone, which is the only day this can mean.
	var today string

	p.evalJSON(`JSON.stringify(todayISO())`, &today)

	if start != today {
		t.Errorf("a new project starts on %q and today is %q", start, today)
	}

	// And it survives making one: the second project asks no more than the first.
	p.run("make one",
		chromedp.SetValue(`#form-project input[name="name"]`, "Dachsanierung", chromedp.ByQuery),
		p.click(`#form-project button[type="submit"]`))

	p.waitForText("#toast", "reated")

	if again := p.value(`#form-project input[name="startDate"]`); again != today {
		t.Errorf("after making a project the start reads %q", again)
	}
}

// A project can be corrected after it is made.
//
// Everything else on this screen could be changed - a project could be completed,
// archived and deleted - except the three things somebody actually gets wrong
// while typing: the name, the period and what it is for. The API has taken all
// three all along; there was no way to ask for it.
//
// And the two optional fields have to be emptiable, which is the half that is
// easy to miss. A project given an end date by mistake, or one that turned out to
// be ongoing after all, has to be able to go back to having none - and an empty
// date is not silence, it is a request.
func TestAProjectCanBeCorrectedAndItsOptionalFieldsEmptied(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	p.run("make one", p.click(`.tab[data-view="projects"]`),
		chromedp.WaitVisible("#form-project", chromedp.ByID),
		chromedp.SetValue(`#form-project input[name="name"]`, "Dachsanierung",
			chromedp.ByQuery),
		chromedp.SetValue(`#form-project input[name="endDate"]`, "2027-03-31",
			chromedp.ByQuery),
		chromedp.SetValue(`#form-project input[name="description"]`, "Ziegel und Rinnen",
			chromedp.ByQuery),
		p.click(`#form-project button[type="submit"]`))

	p.waitForText("#table-projects tbody", "Dachsanierung")

	p.run("open it", p.click(`#table-projects tbody button[data-action="edit"]`),
		chromedp.WaitVisible("#project-cancel", chromedp.ByID))

	if got := p.value(`#form-project input[name="name"]`); got != "Dachsanierung" {
		t.Errorf("the form opened on %q rather than on the project that was picked", got)
	}

	if got := p.value(`#form-project input[name="endDate"]`); got != "2027-03-31" {
		t.Errorf("the form opened with an end of %q", got)
	}

	if got := p.value(`#form-project input[name="description"]`); got != "Ziegel und Rinnen" {
		t.Errorf("the form opened with a description of %q", got)
	}

	// Emptied the way a person empties them. A date field is two boxes - the one
	// that is typed in and the native one behind it - and clearing the visible one
	// is what clears the date; setting the native value from outside would be
	// testing a path nobody uses.
	var emptied string

	p.run("empty what was optional", chromedp.Evaluate(`
		(() => {
			const native = document.querySelector('#form-project input[name="endDate"]');
			const shown = native.closest('.date-wrap').querySelector('.date-shown');

			shown.value = '';
			shown.dispatchEvent(new Event('input', { bubbles: true }));

			const about = document.querySelector('#form-project input[name="description"]');

			about.value = '';
			about.dispatchEvent(new Event('input', { bubbles: true }));

			return native.value + '|' + about.value;
		})()`, &emptied))

	if emptied != "|" {
		t.Fatalf("the fields did not empty (%q), so nothing below proves anything",
			emptied)
	}

	p.run("correct the name and save",
		chromedp.SetValue(`#form-project input[name="name"]`, "Dachsanierung Nord",
			chromedp.ByQuery),
		p.click(`#form-project button[type="submit"]`))

	p.waitForText("#table-projects tbody", "Dachsanierung Nord")

	row := p.text("#table-projects tbody")

	// The end is gone rather than kept, which is the half a partial update gets
	// wrong: an empty field that is simply dropped leaves the old value standing.
	if strings.Contains(row, "2027") {
		t.Errorf("the end date survived being emptied: %q", row)
	}

	// And the description with it - as no description, which is a dash, rather
	// than as an empty one, which is a cell that reads like a fault.
	if strings.Contains(row, "Ziegel") {
		t.Errorf("the description survived being emptied: %q", row)
	}

	if !strings.Contains(row, "–") {
		t.Errorf("the emptied fields left blank cells rather than saying there is "+
			"nothing there: %q", row)
	}

	// The form goes back to making one, rather than sitting on the project just
	// saved - which is how the next new project gets written over an existing one.
	if p.visible("#project-cancel") {
		t.Error("the form stayed in editing after saving")
	}

	if p.value(`#form-project input[name="name"]`) != "" {
		t.Error("the form kept the project it was editing after saving it")
	}

	// And the start is filled in again, the same courtesy a first project gets.
	if p.value(`#form-project input[name="startDate"]`) == "" {
		t.Error("the start was left empty for the next project")
	}
}
