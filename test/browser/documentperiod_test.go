//go:build browser

package browser

import (
	"encoding/json"
	"strconv"
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
func TestAnEvaluationDocumentIsHeadedWithThePeriodItsFiguresAreFor(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Every document request is caught and answered with nothing, so what is
	// checked is what the page asked for rather than how the server laid it out.
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

		var before int

		p.run("count the documents", chromedp.Evaluate(`window.__documents.length`, &before))

		p.run("set the boxes to April and export", chromedp.Evaluate(`
			setDateField(document.querySelector('`+screen.from+`'), '2026-04-01');
			setDateField(document.querySelector('`+screen.to+`'), '2026-04-30')`, nil),
			p.click(screen.button))

		deadline := time.Now().Add(waitPatience)

		var asked struct {
			Subtitle string
			Want     string
		}

		for time.Now().Before(deadline) {
			var raw string

			p.run("read the last document", chromedp.Evaluate(`window.__documents.length > `+
				strconv.Itoa(before)+` ? JSON.stringify({ subtitle: window.__documents.at(-1).subtitle,
					want: fmtDate('2026-03-01') + ' – ' + fmtDate('2026-03-31') }) : ''`, &raw))

			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &asked); err != nil {
					t.Fatalf("reading the document: %v", err)
				}

				break
			}

			time.Sleep(100 * time.Millisecond)
		}

		if asked.Want == "" {
			t.Fatalf("%s never asked for a document", screen.button)
		}

		if asked.Subtitle != asked.Want {
			t.Errorf("%s: the document is headed %q over figures for %q", screen.button,
				asked.Subtitle, asked.Want)
		}
	}
}
