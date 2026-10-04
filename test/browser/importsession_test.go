//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// An import checked by one account leaves nothing behind for the next.
//
// The import cards are not forms, so the sign-out's form reset never reached
// them: the file stayed chosen, the verdict stayed written and the Import button
// stayed offered - a clean verdict brings it with it. The checked rows went too
// little further. The time entries' table is emptied with every other table, and
// one line later the sign-out redraws for the language, which drew the kept
// verdict straight back; the tables' own cards were never emptied at all. So
// whoever signed in next found the last person's rows - their projects and notes
// and hours - and a button that would write them into their own account.
func TestASignOutTakesAnImportsFileAndVerdictWithIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	entries, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "A note of somebody else's"},
	})
	if err != nil {
		t.Fatalf("building the entries: %v", err)
	}

	projects, err := spreadsheet.WriteProjects("en", []spreadsheet.ProjectRow{
		{Name: "A project of somebody else's", StartDate: time.Now()},
	})
	if err != nil {
		t.Fatalf("building the projects: %v", err)
	}

	entriesPath := filepath.Join(dir, "entries.xlsx")
	projectsPath := filepath.Join(dir, "projects.xlsx")

	for path, book := range map[string][]byte{entriesPath: entries, projectsPath: projects} {
		if err := os.WriteFile(path, book, 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	p := open(t)
	p.readyWorker()

	const card = `#view-projects .card:has(input[type="file"])`

	p.run("check a file of time entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{entriesPath}, chromedp.ByQuery))
	p.waitShown("#wb-preview")
	p.run("press check", p.click("#wb-preview"))
	p.waitForText("#table-workbook tbody", "A note of somebody else's")

	p.run("check a file of projects", p.click(`.tab[data-view="projects"]`),
		chromedp.WaitVisible(card, chromedp.ByQuery),
		chromedp.SetUploadFiles(card+` input[type="file"]`, []string{projectsPath}, chromedp.ByQuery),
		p.click(card+` button[data-i18n="wb.preview"]`))
	p.waitForText(card+" tbody", "A project of somebody else's")

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left struct {
		Rows, Files int
		Offered     bool
		Said        string
	}

	p.run("read what the next account would find", chromedp.Evaluate(`(() => {
		const cards = ['#workbook-card', '`+card+`'].map((s) => document.querySelector(s));

		return {
			rows: cards.filter((c) => /somebody else's/.test(c.querySelector('tbody').textContent)).length,
			files: cards.reduce((n, c) => n + c.querySelector('input[type="file"]').files.length, 0),
			offered: cards.some((c) => !c.querySelector('button[data-i18n="wb.import"]').hidden),
			said: cards.map((c) => c.querySelector('p.muted:not([data-i18n])')?.textContent ?? '').join(''),
		};
	})()`, &left))

	if left.Rows > 0 {
		t.Errorf("%d import card(s) still show the rows the last account checked", left.Rows)
	}

	if left.Files > 0 {
		t.Errorf("%d file(s) are still chosen for whoever signs in next", left.Files)
	}

	if left.Offered {
		t.Error("an Import button is still offered for the last account's file")
	}

	if left.Said != "" {
		t.Errorf("the verdict on the last account's file is still written: %q", left.Said)
	}
}

// An evaluation run by one account leaves nothing behind for the next.
//
// An evaluation is computed when somebody asks for one, and nothing on the next
// sign-in asks again: the result card stayed up with the last account's total,
// its target and booked hours, and the picture of where their time went, for
// whoever opened the screen next. The sign-out emptied the tables and redrew the
// rows back into them.
func TestASignOutTakesTheLastEvaluationWithIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("evaluate", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-report", chromedp.ByID),
		p.click(`#form-report button[type="submit"]`))
	p.waitShown("#report-result")

	p.run("work out the balance", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#form-overtime", chromedp.ByID),
		p.click(`#form-overtime button[type="submit"]`))
	p.waitShown("#overtime-result")

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left []string

	p.run("read what the next account would find", chromedp.Evaluate(`(() => {
		const left = [];

		for (const card of ['#report-result', '#overtime-result']) {
			if (!document.querySelector(card).hidden) left.push(card + ' is up');
		}

		for (const text of ['#report-total', '#overtime-total', '#overtime-meta', '#report-chart-caption', '#statistics-total']) {
			if (document.querySelector(text).textContent.trim()) left.push(text + ' says ' + document.querySelector(text).textContent);
		}

		for (const holder of ['#table-report tbody', '#table-overtime tbody', '#report-chart', '#chart-days', '#chart-projects']) {
			if (document.querySelector(holder).childElementCount) left.push(holder + ' holds a drawing');
		}

		return left;
	})()`, &left))

	for _, what := range left {
		t.Errorf("after the sign-out %s", what)
	}
}
