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

// A form left correcting a record goes back to creating one when its account
// signs out.
//
// The sign-out resets every form, and a reset leaves a hidden field's value
// where it was - which is why each form's own reset clears its id by hand. Those
// resets were not called on the way out, so a form somebody had been correcting
// kept the record's id under its "edit" heading. Whoever signed in next and
// filled it in to make something new changed the record the last person had
// open: an administrator adding an account overwrote the one their predecessor
// was correcting.
func TestASignOutPutsEveryFormBackToCreating(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// The administrator's own row offers no correction, so somebody else's first.
	p.run("add somebody", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Wilma", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "wilma@example.com", chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "wilma-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`))
	p.waitForText("#table-users tbody", "wilma@example.com")

	p.run("start correcting the account", p.click(`#table-users tbody button[data-action="edit"]`))

	// A shipped role is opened to be looked at, through the same form and with
	// its id in the same field.
	p.run("open a role", p.click(`.tab[data-view="roles"]`),
		chromedp.WaitVisible(`#table-roles tbody td.actions button`, chromedp.ByQuery),
		p.click(`#table-roles tbody td.actions button`))

	// Read from the forms rather than the screen: only one of the two tabs is
	// showing.
	var correcting bool

	p.run("confirm both forms are correcting", chromedp.Evaluate(`[['user', 'Add user'], ['role', 'Create role']]
		.every(([kind, creating]) => document.querySelector('#form-' + kind).elements.id.value
			&& document.querySelector('#' + kind + '-form-title').textContent.trim() !== creating)`, &correcting))

	if !correcting {
		t.Fatal("this case starts from both forms correcting a record, and they are not")
	}

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left []string

	p.run("read what the next account would find", chromedp.Evaluate(`(() => {
		const left = [];

		for (const [kind, creating] of [['user', 'Add user'], ['role', 'Create role']]) {
			const form = document.querySelector('#form-' + kind);
			const heading = document.querySelector('#' + kind + '-form-title').textContent.trim();

			if (form.elements.id.value) left.push('the ' + kind + ' form still holds the id ' + form.elements.id.value);
			if (heading !== creating) left.push('the ' + kind + ' form is still headed ' + JSON.stringify(heading));
		}

		return left;
	})()`, &left))

	for _, what := range left {
		t.Errorf("after the sign-out %s", what)
	}
}
