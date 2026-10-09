//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// An evaluation as a document is headed with the period its figures are for.
//
// The heading was read off the form's date boxes when the button was pressed,
// and the figures off the result on screen - two different moments. Somebody
// who evaluated March and then set the boxes to April, to look at it next, got a
// document headed April over March's table, total and pictures; and with the
// boxes left empty, which lets the server choose the period, the document named
// no period at all. Each answer says which period it was worked out for.
//
// The period ends the heading rather than being all of it, because a report
// names what it covers first - TestAReportDocumentNamesWhatItsFiguresCover.
func TestAnEvaluationDocumentIsHeadedWithThePeriodItsFiguresAreFor(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.catchDocuments()

	for _, screen := range []struct {
		tab, from, to, ask, result, button string
	}{
		{"report", `#form-report [name="from"]`, `#form-report [name="to"]`,
			`#form-report button[type="submit"]`, "#report-result", "#report-pdf"},
		{"overtime", `#form-overtime [name="from"]`, `#form-overtime [name="to"]`,
			`#form-overtime button[type="submit"]`, "#overtime-result", "#overtime-pdf"},
		{"overtime", "#statistics-from", "#statistics-to", "#statistics-load", "#statistics-card", "#statistics-pdf"},
	} {
		p.run("evaluate March on "+screen.button, p.click(`.tab[data-view="`+screen.tab+`"]`),
			chromedp.Evaluate(`setDateField(document.querySelector('`+screen.from+`'), '2026-03-01');
				setDateField(document.querySelector('`+screen.to+`'), '2026-03-31')`, nil),
			p.click(screen.ask))
		p.waitShown(screen.result)
		p.atRest()

		before := p.documentsAsked()

		p.run("set the boxes to April and export", chromedp.Evaluate(`
			setDateField(document.querySelector('`+screen.from+`'), '2026-04-01');
			setDateField(document.querySelector('`+screen.to+`'), '2026-04-30')`, nil),
			p.click(screen.button))

		subtitle := p.subtitleAsked(before, screen.button)

		var want string

		p.run("write March the way the screen does", chromedp.Evaluate(
			`fmtDate('2026-03-01') + ' – ' + fmtDate('2026-03-31')`, &want))

		if !strings.HasSuffix(subtitle, want) {
			t.Errorf("%s: the document is headed %q over figures for %q", screen.button,
				subtitle, want)
		}
	}
}

// A report as a document names what its figures cover: every project, one of
// them, or the hours booked on none.
//
// It said only the period, so a page printed for one project could not be told
// from one printed for another, or for all of them, while the screen it came from
// had the choice in plain sight. And like the period, what it names is what the
// figures were asked for rather than what the select says by the time the button
// is pressed: somebody who evaluated one project and then picked the next, to look
// at it after, would otherwise put the next one's name over the first one's
// figures - which is why the select is moved on before every export here.
func TestAReportDocumentNamesWhatItsFiguresCover(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// A different total for each choice - an hour on the project, two on none,
	// three in all - so waiting for an evaluation to land is waiting for its own
	// figures rather than for a result card that is already showing.
	var projectID int

	p.run("book on a project and on none", chromedp.Evaluate(`(async () => {
		const project = await api('/projects', { method: 'POST', body: JSON.stringify({
			name: 'Dachsanierung', startDate: '2026-03-01' }) });

		await api('/timesheets', { method: 'POST', body: JSON.stringify({
			projectId: project.id, date: '2026-03-10', durationHours: 1 }) });
		await api('/timesheets', { method: 'POST', body: JSON.stringify({
			date: '2026-03-11', durationHours: 2 }) });

		return project.id;
	})()`, &projectID, awaitPromise))

	id := strconv.Itoa(projectID)

	// Booked behind the screen's back, so the screen is loaded again to offer it.
	p.reload()
	p.waitForNode(`#form-report [name="projectId"] option[value="` + id + `"]`)
	p.catchDocuments()

	p.run("open the report on March", p.click(`.tab[data-view="report"]`),
		chromedp.Evaluate(`setDateField(document.querySelector('#form-report [name="from"]'), '2026-03-01');
			setDateField(document.querySelector('#form-report [name="to"]'), '2026-03-31')`, nil))

	for _, scope := range []struct {
		value, hours, next, names string
	}{
		{id, "1", "none", `'Dachsanierung'`},
		{"none", "2", "", `t('report.noProject', 'No project')`},
		{"", "3", id, `t('filter.allProjects', 'All projects')`},
	} {
		p.run("evaluate "+scope.names, chromedp.Evaluate(
			`document.querySelector('#form-report [name="projectId"]').value = '`+scope.value+`'`, nil),
			p.click(`#form-report button[type="submit"]`))

		p.waitEvaluates("the figures for "+scope.names, `String(
			document.querySelector('#report-total').textContent ===
				t('report.total', '{0} in total').replace('{0}', fmtHours(`+scope.hours+`)))`, "true")
		p.atRest()

		before := p.documentsAsked()

		p.run("pick the next and export", chromedp.Evaluate(
			`document.querySelector('#form-report [name="projectId"]').value = '`+scope.next+`'`, nil),
			p.click("#report-pdf"))

		subtitle := p.subtitleAsked(before, "#report-pdf")

		var names string

		p.run("word what it covers", chromedp.Evaluate(scope.names, &names))

		if !strings.HasPrefix(subtitle, names) {
			t.Errorf("a report of %s is headed %q", names, subtitle)
		}
	}
}

// catchDocuments answers every document request with nothing and keeps what the
// page asked for, so a case checks what was asked to be laid out rather than how
// the server laid it out.
func (p *page) catchDocuments() {
	p.t.Helper()

	p.run("catch the documents", chromedp.Evaluate(`(() => {
		window.__documents = [];

		const real = window.fetch;
		window.fetch = async (input, init) => {
			const url = typeof input === 'string' ? input : input.url;

			if (url.includes('/exports/document')) {
				window.__documents.push(JSON.parse(init.body));

				return new Response(new Blob(['%PDF-1.4']), { status: 200, headers: { 'Content-Type': 'application/pdf' } });
			}

			return real(input, init);
		};

		return 1;
	})()`, nil))
}

// documentsAsked is how many documents the page has asked for so far.
func (p *page) documentsAsked() int {
	p.t.Helper()

	var asked int

	p.run("count the documents", chromedp.Evaluate(`window.__documents.length`, &asked))

	return asked
}

// subtitleAsked waits for a document after the first before, and returns the
// line under its title.
func (p *page) subtitleAsked(before int, button string) string {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		var arrived bool

		p.run("look for the document", chromedp.Evaluate(
			`window.__documents.length > `+strconv.Itoa(before), &arrived))

		if arrived {
			var subtitle string

			p.run("read its heading", chromedp.Evaluate(`window.__documents.at(-1).subtitle ?? ''`, &subtitle))

			return subtitle
		}

		time.Sleep(100 * time.Millisecond)
	}

	p.t.Fatalf("%s never asked for a document", button)

	return ""
}
