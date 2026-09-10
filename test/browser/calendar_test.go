//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Clicking a time entry in the calendar has to open it for correcting.
//
// The calendar could be read but not acted on: the day opened a table of
// entries, and there it stopped. Worse, the interface had no way to change an
// entry at all - the API has taken a full update from the beginning, but the only
// route the screen offered was deleting the entry and typing it again. So this
// covers both halves: the row opens the entry, and saving it corrects that entry
// instead of booking a second one.
func TestACalendarEntryOpensForEditing(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// On today, so it lands in the month the calendar opens on - the date field
	// already points there.
	p.run("book an entry",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "3.5", chromedp.ByQuery),
		chromedp.SendKeys(`#form-timesheet input[name="description"]`,
			"Typed with a slip", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	p.run("open today in the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible(".cal-day.has-entries", chromedp.ByQuery),
		p.click(".cal-day.has-entries"),
		chromedp.WaitVisible("#table-calendar-day tbody tr.clickable", chromedp.ByQuery),
	)

	// A cell rather than the row, so the click cannot land on one of the action
	// buttons the row also carries.
	p.run("click the entry", p.click("#table-calendar-day tbody tr.clickable td"))

	p.waitShown("#view-timesheets")

	if !p.visible("#view-timesheets") {
		t.Fatalf("clicking an entry should bring up the form that edits it\n\napplication log:\n%s",
			p.app.Log())
	}

	if id := p.value(`#form-timesheet input[name="id"]`); id == "" {
		t.Error("the form is not pointed at the entry that was clicked")
	}

	if got := p.value(`#form-timesheet input[name="durationHours"]`); got != "3.5" {
		t.Errorf("the form holds %q hours, want the entry's 3.5", got)
	}

	if got := p.value(`#form-timesheet input[name="description"]`); got != "Typed with a slip" {
		t.Errorf("the form holds the description %q, want the entry's", got)
	}

	// Saving the correction has to change that entry, not add another one.
	p.run("correct the hours",
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "4.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "4.25") {
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	var rows int

	p.run("count the entries", chromedp.Evaluate(
		`document.querySelectorAll('#table-timesheets tbody tr').length`, &rows))

	if rows != 1 {
		t.Errorf("there are %d entries after correcting one; the save booked a second entry",
			rows)
	}

	if body := p.text("#table-timesheets tbody"); !strings.Contains(body, "4.25") {
		t.Errorf("the corrected hours never reached the table\n\ntable:\n%s\n\napplication log:\n%s",
			body, p.app.Log())
	}

	// And the form goes back to booking, or the next entry typed would silently
	// overwrite the one just corrected.
	if id := p.value(`#form-timesheet input[name="id"]`); id != "" {
		t.Errorf("the form is still pointed at entry %s after saving", id)
	}
}
