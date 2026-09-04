//go:build browser

package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A chart says what it is.
//
// All three drawings carry role="img", which is the right choice: a screen reader
// working through forty <text> nodes one after another is not a chart, it is a
// list of numbers in drawing order. But the role makes the drawing one opaque
// node, so the labels and figures inside it stop being readable - and none of the
// three gave it a name to read instead. The one element on these screens that is
// entirely picture was announced as "image" and nothing more.
//
// The words exist already and are already translated: a heading above each chart
// on the statistics screen, a caption above the one under an evaluation. This
// checks that the drawing is named by them rather than by a second sentence
// written beside them, which would be a second thing to translate and a second
// thing to keep in step.
func TestEveryChartIsAnnouncedByTheWordsAboveIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Something to draw: an empty period draws nothing at all, and a chart that
	// was never drawn would pass this without being named.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`,
			"3.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "3.25")

	from, to, _ := thisMonth()

	p.run("evaluate the month", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#statistics-card", chromedp.ByID),
		chromedp.SetValue("#statistics-from", from, chromedp.ByID),
		chromedp.SetValue("#statistics-to", to, chromedp.ByID),
		p.click("#statistics-load"))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#chart-days svg .chart-bar") {
		t.Fatal("nothing was drawn, so there is no chart to be named")
	}

	// And the one under an evaluation, which is the other markup shape: a caption
	// rather than a heading, and no .chart-wrap around it, so it exercises the
	// branch that falls back to the container itself.
	p.run("evaluate", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-report", chromedp.ByID),
		p.click(`#form-report button[type="submit"]`),
		chromedp.WaitVisible("#report-result", chromedp.ByID))

	deadline = time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#report-chart svg") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	for _, chart := range []string{"#chart-days", "#chart-projects", "#report-chart"} {
		var out string

		p.run("read how "+chart+" is announced", chromedp.Evaluate(`(() => {
			const container = document.querySelector('`+chart+`');
			const drawing = container.querySelector('svg');
			const naming = (container.closest('.chart-wrap') ?? container)
				.previousElementSibling;

			return JSON.stringify({
				role: drawing.getAttribute('role') ?? '',
				label: (drawing.getAttribute('aria-label') ?? '').trim(),
				above: (naming?.textContent ?? '').trim(),
			});
		})()`, &out))

		var drawing struct {
			Role  string `json:"role"`
			Label string `json:"label"`
			Above string `json:"above"`
		}

		if err := json.Unmarshal([]byte(out), &drawing); err != nil {
			t.Fatalf("reading %s (%q): %v", chart, out, err)
		}

		if drawing.Role == "" {
			t.Fatalf("%s was not drawn, so there is nothing to be named", chart)
		}

		if drawing.Above == "" {
			t.Fatalf("%s has no words above it in the markup, so this case cannot "+
				"say what it should be named by", chart)
		}

		if drawing.Label == "" {
			t.Errorf("%s carries role=%q and no aria-label, so the role hides the "+
				"labels inside the drawing and puts nothing in their place. The "+
				"heading above it already says %q",
				chart, drawing.Role, drawing.Above)

			continue
		}

		if !strings.Contains(drawing.Label, drawing.Above) {
			t.Errorf("%s is announced as %q while the words above it say %q; that is "+
				"two names for one chart, and only one of them is translated with "+
				"the markup", chart, drawing.Label, drawing.Above)
		}
	}
}
