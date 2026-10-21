//go:build browser

package browser

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// typeIntoCell replaces what a grid cell holds and leaves it, which is what
// commits it - the way a person tabs into a cell, types over what it holds and
// tabs on.
//
// Typed into whatever has the focus rather than into the field found afresh, and
// never emptied from a script first: a field emptied that way and then drawn again
// held its number once more, and the keys went in front of it - 3 typed over 2.5
// arrived as 32.5.
func (p *page) typeIntoCell(selector, value string) {
	p.t.Helper()

	p.run("type "+value+" into "+selector,
		chromedp.Focus(selector, chromedp.ByQuery),
		chromedp.Evaluate(`document.activeElement.select()`, nil),
		chromedp.KeyEvent(value+kb.Tab))
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
	// a second - once the grid knows the entry, which is the redraw after the save.
	p.waitForNode(cell + `:not([data-id=""])`)
	p.typeIntoCell(cell, "3")

	p.waitFor("the Monday to be corrected", func() bool {
		hours := p.entriesOn(monday)

		return len(hours) == 1 && hours[0] == 3
	})

	// Waited for rather than read once: the server holding the number is not the
	// grid having been drawn from it, which is a request later.
	p.waitForText("#table-week tfoot tr:first-child", "3")

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

// A number being typed survives the grid being drawn again under it, which is
// what happens when the cell before it was just saved and the week comes back
// from the server while the next one is being filled in: the digits after the
// redraw go after the ones before it, and leaving the field still books it.
func TestTypingSurvivesTheGridBeingDrawnAgain(t *testing.T) {
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

	tuesday := addISODays(t, monday, 1)

	p.run("open the week", p.click(`.tab[data-view="week"]`),
		chromedp.WaitVisible("#table-week", chromedp.ByID))

	p.waitForNode(`#week-add-project option[value="` + strconv.Itoa(projectID) + `"]`)
	p.run("add a row for the project", chromedp.Evaluate(`(() => {
		const select = document.querySelector('#week-add-project');
		select.value = '`+strconv.Itoa(projectID)+`';
		select.dispatchEvent(new Event('change', { bubbles: true }));
	})()`, nil))

	cellOf := func(day string) string {
		return `#table-week input[data-cell="` + strconv.Itoa(projectID) + `|` + day + `"]`
	}

	// Half a number, the grid drawn again, the other half: typed into whatever has
	// the focus, the way a keyboard does, rather than into a field found afresh.
	p.waitForNode(cellOf(monday))
	p.run("type half of the Monday", chromedp.Focus(cellOf(monday), chromedp.ByQuery), chromedp.KeyEvent("1"))
	p.run("draw the grid again", chromedp.Evaluate(`drawWeekGrid()`, nil))
	p.run("type the rest and leave", chromedp.KeyEvent(".5"+kb.Tab))

	p.waitFor("the Monday to be booked as typed", func() bool {
		return slices.Equal(p.entriesOn(monday), []float64{1.5})
	})

	// A whole number, the grid drawn again, and nothing more typed before leaving:
	// still booked, although the field holding it is no longer the one typed into.
	p.waitForNode(cellOf(monday) + `:not([data-id=""])`)
	p.run("type the Tuesday", chromedp.Focus(cellOf(tuesday), chromedp.ByQuery), chromedp.KeyEvent("3"))
	p.run("draw the grid again", chromedp.Evaluate(`drawWeekGrid()`, nil))
	p.run("leave the Tuesday", chromedp.KeyEvent(kb.Tab))

	p.waitFor("the Tuesday to be booked", func() bool {
		return slices.Equal(p.entriesOn(tuesday), []float64{3})
	})
}

// Last week is copied from the grid, after asking: its hours land on the same
// days of this one, and the grid and the notice both say so.
func TestLastWeekIsCopiedFromTheGrid(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var monday string

	p.awaitJSON(`mondayOf(todayISO())`, &monday)

	lastMonday := addISODays(t, monday, -7)

	p.awaitJSON(`api('/timesheets', {
		method: 'POST', body: JSON.stringify({ date: '`+lastMonday+`', durationHours: 2.5 }),
	}).then(() => true)`, nil)

	p.run("open the week", p.click(`.tab[data-view="week"]`),
		chromedp.WaitVisible("#table-week", chromedp.ByID))

	p.run("copy last week", p.click("#week-copy"),
		chromedp.WaitVisible(".confirm-card", chromedp.ByQuery),
		p.click(".confirm-card button.confirm-proceed"))

	p.waitFor("last Monday's hours on this Monday", func() bool {
		return slices.Equal(p.entriesOn(monday), []float64{2.5})
	})

	p.waitForNode(`#table-week input[data-cell="none|` + monday + `"][data-value="2.5"]`)

	if news := p.text(".toast-note"); !strings.Contains(news, "Entries copied: 1") {
		t.Errorf("the notice reads %q, want how many entries were copied", news)
	}
}

// addISODays moves a calendar day written as YYYY-MM-DD.
func addISODays(t *testing.T, day string, days int) string {
	t.Helper()

	parsed, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatalf("read %q as a day: %v", day, err)
	}

	return parsed.AddDate(0, 0, days).Format(time.DateOnly)
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

// From a tablet's width on, the whole week is in view on the grid.
//
// A day's field was 5.5em at every width, so the seven of them came to about 880
// pixels before a project's name had any room, and an iPad held upright gives the
// card 690. Saturday, Sunday and the week's total lay past the card's edge,
// reached by a swipe nothing announced - a touch screen draws no scroll bar - so
// the week looked as though it ended on Friday. A telephone still scrolls the
// grid, as it has to; from 768 pixels there is room for all seven days once the
// fields stop holding width their figures do not need.
//
// Four projects, one with a long name, and a day away whose name is written under
// its date, in both languages.
func TestTheWholeWeekIsInViewOnATablet(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`(async () => {
		const monday = mondayOf(todayISO());
		const day = (n) => {
			const d = new Date(monday + 'T12:00:00Z');
			d.setUTCDate(d.getUTCDate() + n);
			return d.toISOString().slice(0, 10);
		};

		for (const [name, hours] of [['Customer support and escalations', 2.25], ['Website relaunch', 3.75],
			['Internal tooling', 1.5], ['Training', 0.5]]) {
			const made = await api('/projects', {method: 'POST', body: JSON.stringify({name, startDate: '2026-01-01'})});

			for (const n of [0, 1, 2]) {
				await api('/timesheets', {method: 'POST', body: JSON.stringify({projectId: made.id, date: day(n), durationHours: hours})});
			}
		}

		await api('/me/absences', {method: 'POST', body: JSON.stringify({from: day(4), to: day(4), kind: 'vacation'})});
		await loadProjects();

		return true;
	})()`, nil)

	p.run("open the week", p.click(`.tab[data-view="week"]`),
		chromedp.WaitVisible("#table-week", chromedp.ByID))
	p.waitForRows("#table-week tbody tr", 4)

	for _, language := range []string{"de", "en"} {
		p.run("a desktop", chromedp.EmulateViewport(1280, 900))
		p.chooseLanguage(language)

		for _, size := range []struct{ w, h int64 }{{768, 1024}, {820, 1180}, {1024, 768}} {
			p.run(fmt.Sprintf("%dx%d", size.w, size.h), chromedp.EmulateViewport(size.w, size.h))

			var fit struct {
				Wide float64 `json:"wide"`
				Room float64 `json:"room"`
			}

			p.evalJSON(`(() => {
				const wrap = document.querySelector('#week-card .table-wrap');
				return JSON.stringify({wide: wrap.scrollWidth, room: wrap.clientWidth});
			})()`, &fit)

			if fit.Wide > fit.Room+1 {
				t.Errorf("in %s at %dx%d the week is %.0fpx wide in %.0fpx, so its end is out of view",
					language, size.w, size.h, fit.Wide, fit.Room)
			}
		}
	}
}
