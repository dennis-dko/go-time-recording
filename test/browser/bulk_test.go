//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Deleting several rows at once.
//
// Only a browser can check this. The checkbox column is not written into the
// markup: it is derived, at render time, from whether a row carries a delete
// button - so whether the column appears at all, whether "select all" reaches
// every row, and whether the bar knows how many are ticked are all facts about
// running JavaScript. Reading app.js proves none of them.
//
// What would go wrong quietly: a column that appears but ticks nothing, or a bar
// that deletes one row while claiming three.
func TestSeveralRowsAreDeletedAtOnce(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Three entries of the administrator's own, so nothing else has to exist.
	hours := []string{"1.11", "2.22", "3.33"}

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	for _, figure := range hours {
		p.run("book "+figure,
			chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, figure,
				chromedp.ByQuery),
			p.click(`#form-timesheet button[type="submit"]`))

		p.waitForText("#table-timesheets tbody", figure)
	}

	// The column exists because the rows offer deletion, and there is one checkbox
	// per row rather than one for the table.
	if got := p.count("#table-timesheets tbody input.row-pick"); got != len(hours) {
		t.Fatalf("the table has %d row checkboxes for %d deletable rows", got, len(hours))
	}

	// Nothing is ticked yet, so the bar is not in the way.
	if p.visible(".bulk-bar.shown") {
		t.Error("the bulk bar is showing with nothing selected")
	}

	// "Select all" reaches every row.
	p.run("select all", p.click(`#table-timesheets thead input.pick-all`),
		chromedp.WaitVisible(".bulk-bar.shown", chromedp.ByQuery))

	if got := p.count("#table-timesheets tbody input.row-pick:checked"); got != len(hours) {
		t.Fatalf(`"select all" ticked %d of %d rows`, got, len(hours))
	}

	// And the bar counts what is ticked rather than guessing.
	if count := p.text(".bulk-bar.shown .bulk-count"); !strings.Contains(count, "3") {
		t.Errorf("the bar says %q, which does not mention 3 selected rows", count)
	}

	// Deleting asks once for the batch, not once per row.
	p.run("press delete", p.click(`.bulk-bar.shown button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("confirm", p.click(`.confirm-actions button.danger`))
	p.waitGone(".confirm-overlay")

	// All three gone, and the column with them: there is nothing left to delete.
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		body := p.text("#table-timesheets tbody")

		gone := true
		for _, figure := range hours {
			if strings.Contains(body, figure) {
				gone = false

				break
			}
		}

		if gone {
			if p.visible(".bulk-bar.shown") {
				t.Error("the bulk bar is still showing after the rows it referred to went")
			}

			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("the rows are still there after a bulk delete; the table says:\n%s\n\n"+
		"application log:\n%s", p.text("#table-timesheets tbody"), p.app.Log())
}

// Deleting some of the rows leaves the bar telling the truth about the rest.
//
// This is the case that was wrong. The bar reads how many checkboxes are ticked out
// of the table, and it was refreshed before the new rows were in the document - so
// it counted the previous render. Delete two of three and the bar stayed up saying
// "2 selected" over a table where nothing was ticked, with a delete button that
// would have done nothing.
//
// Deleting all of them hid it by luck: with no rows left there is nothing to delete,
// so the whole column goes and the bar with it.
func TestTheBarStandsDownWhenOnlySomeRowsWereDeleted(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	hours := []string{"4.11", "5.22", "6.33"}

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	for _, figure := range hours {
		p.run("book "+figure,
			chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, figure,
				chromedp.ByQuery),
			p.click(`#form-timesheet button[type="submit"]`))

		p.waitForText("#table-timesheets tbody", figure)
	}

	// Two of the three, by ticking them one at a time rather than select-all.
	p.run("tick the first two",
		p.click(`#table-timesheets tbody tr:nth-child(1) input.row-pick`),
		p.click(`#table-timesheets tbody tr:nth-child(2) input.row-pick`),
		chromedp.WaitVisible(".bulk-bar.shown", chromedp.ByQuery))

	if got := p.count("#table-timesheets tbody input.row-pick:checked"); got != 2 {
		t.Fatalf("%d rows are ticked, want 2", got)
	}

	p.run("delete them", p.click(`.bulk-bar.shown button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
		p.click(`.confirm-actions button.danger`))

	p.waitGone(".confirm-overlay")

	// One row is left, so the column stays - and the bar must have stood down,
	// because nothing in the new table is ticked.
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		if p.count("#table-timesheets tbody input.row-pick") == 1 {
			if p.visible(".bulk-bar.shown") {
				t.Errorf("one row is left and nothing is ticked, but the bar is still up "+
					"saying %q", p.text(".bulk-bar.shown .bulk-count"))
			}

			if got := p.count("#table-timesheets tbody input.row-pick:checked"); got != 0 {
				t.Errorf("%d checkbox(es) survived the reload, so a second delete would "+
					"act on a selection nobody made", got)
			}

			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("the table does not hold exactly one deletable row after deleting two of "+
		"three; it says:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}

// A row nobody may delete gets no checkbox, and the ones beside it still line up.
//
// The cell is still there for such a row - it just has nothing in it to tick -
// because a missing cell would shift every column of that row one to the left. That
// is the kind of fault that looks like bad data rather than bad markup.
func TestARowThatCannotBeDeletedHasNoCheckboxButKeepsItsPlace(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// A second account, so the table holds one row that may be deleted and one that
	// may not - the built-in administrator. With only the administrator there would
	// be nothing deletable at all and no column, which is a different case.
	p.run("add somebody", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Wilma", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "wilma@example.com",
			chromedp.ByQuery),
		chromedp.SetValue(`#form-user select[name="role"]`, "employee", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="password"]`, "wilma-password-1",
			chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`))

	p.waitForText("#table-users tbody", "wilma@example.com")

	// Two rows, two pick cells: the administrator's is there to keep the columns
	// lined up, which is why the cell is added even where nothing may be ticked.
	if got := p.count("#table-users tbody td.pick"); got != 2 {
		t.Errorf("%d rows have a checkbox cell, want 2; without one the row's columns "+
			"sit one to the left of the heading", got)
	}

	// And exactly one checkbox: Wilma's, not the administrator's.
	if got := p.count("#table-users tbody input.row-pick"); got != 1 {
		t.Errorf("the table offers %d checkboxes, want 1 - the built-in administrator "+
			"cannot be deleted and must not be offered for it", got)
	}

	// Every row has as many cells as the heading has columns.
	head := p.count("#table-users thead th")
	cells := p.count("#table-users tbody tr td")

	if head == 0 || cells != head*2 {
		t.Errorf("the heading has %d columns and two rows have %d cells between them, "+
			"want %d", head, cells, head*2)
	}
}
