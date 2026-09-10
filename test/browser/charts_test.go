//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The charts are SVG built in the page, because the Content-Security-Policy
// allows no external origin and a chart library from a CDN would simply be
// blocked. That makes them a browser question twice over: whether the elements
// exist, and whether they render - an <svg> built in the HTML namespace parses
// without complaint and draws nothing at all.
func TestTheOwnHoursChartsAreDrawn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Two entries on one day and none on the next, so an empty day has something
	// to be empty about.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "3.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "3.25")

	p.run("open overtime", p.click(`.tab[data-view="overtime"]`))

	// Worth asserting rather than assuming: the click that opens this view landed
	// on a notice instead of the tab until the notices were made transparent to
	// the pointer, and the symptom was a view that simply never opened.
	if !p.visible("#view-overtime") {
		t.Fatal("the overtime view did not open - something is covering the tab")
	}

	if !p.visible("#statistics-card") {
		t.Fatal("the overtime view is open but the statistics card is not visible")
	}

	// An explicit range, so the number of rows below is a fixed expectation - the
	// default is the first of the month to today, which is a different length every
	// day. This month rather than a written-down one: the entry above is booked on
	// today, so a range that names a past month is a month of empty days. See
	// thisMonth.
	from, to, daysInMonth := thisMonth()

	p.run("evaluate",
		chromedp.SetValue("#statistics-from", from, chromedp.ByID),
		chromedp.SetValue("#statistics-to", to, chromedp.ByID),
		p.click("#statistics-load"))

	// A bar exists, and the SVG is in the right namespace - an HTML-namespace
	// <svg> would be found by a selector and occupy no space.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#chart-days svg .chart-bar") {
		t.Fatalf("no bar was drawn for the day chart; the container holds %q",
			truncateText(p.text("#chart-days"), 200))
	}

	var namespace string

	p.run("read the namespace", chromedp.Evaluate(
		`document.querySelector('#chart-days svg')?.namespaceURI ?? ''`, &namespace))

	if namespace != "http://www.w3.org/2000/svg" {
		t.Errorf("the chart is in the %q namespace, so it would render nothing", namespace)
	}

	// The total is on screen, and the figure is the one that was booked.
	if total := p.text("#statistics-total"); !strings.Contains(total, "3.25") {
		t.Errorf("the total says %q, want it to mention 3.25", total)
	}

	// Every day of the month is a row, including the ones with nothing on them:
	// a chart of only the days that have entries reads as a full week.
	var rows int

	p.run("count the rows",
		chromedp.Evaluate(`document.querySelectorAll('#chart-days .chart-track').length`, &rows))

	if rows != daysInMonth {
		t.Errorf("the day chart has %d rows, want %d - one for every day of the month, "+
			"including the empty ones", rows, daysInMonth)
	}

	// And the project chart drew the uncategorised bucket, since the entry has no
	// project - which is an answer rather than a gap.
	if !strings.Contains(p.text("#chart-projects"), "3.25") {
		t.Errorf("the project chart does not show the hours: %q",
			truncateText(p.text("#chart-projects"), 200))
	}
}

// The evaluation can be read as bars, as columns, or as a circle.
//
// Three shapes were asked for and three shapes were built, and nothing looked at
// them again. The switch is one listener writing a field and calling the drawing
// function, which is exactly the kind of thing that survives a refactor by being
// silently disconnected: the buttons stay, the labels stay, and pressing them
// stops changing the picture. Only a browser notices, because the failure is that
// the same SVG comes back.
func TestTheEvaluationDrawsInWhicheverShapeIsChosen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("book an hour", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "2.5",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "2.5")

	p.run("evaluate", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-report", chromedp.ByID),
		p.click(`#form-report button[type="submit"]`),
		chromedp.WaitVisible("#report-result", chromedp.ByID))

	// Bars to begin with, which is the remembered default.
	if shape := p.chartShape(); shape != "rect" {
		t.Errorf("the evaluation opens as %q rather than as bars", shape)
	}

	p.run("draw it as a circle", p.click(`#report-chart-switch button[data-chart="pie"]`))

	// A circle is drawn as paths, or as one circle where a single part is the
	// whole. Either says the shape changed; a rect says the press did nothing.
	if shape := p.chartShape(); shape != "path" && shape != "circle" {
		t.Errorf("pressing the circle drew %q", shape)
	}

	p.run("draw it as columns", p.click(`#report-chart-switch button[data-chart="columns"]`))

	if shape := p.chartShape(); shape != "rect" {
		t.Errorf("pressing columns drew %q", shape)
	}

	// And the pressed one says so, for anybody reading the page rather than
	// looking at it.
	p.run("draw it as a circle again", p.click(`#report-chart-switch button[data-chart="pie"]`))

	var pressed string

	p.run("read which button is pressed", chromedp.Evaluate(
		`document.querySelector('#report-chart-switch button[aria-pressed="true"]')
			?.dataset.chart ?? ''`, &pressed))

	if pressed != "pie" {
		t.Errorf("the pressed button is %q while a circle is drawn", pressed)
	}

	// The German word is "Kreis" and not "Kuchen", which is what it used to say.
	p.chooseLanguage("de")

	p.waitForText("#report-chart-switch", "Kreis")

	if labels := p.text("#report-chart-switch"); strings.Contains(labels, "Kuchen") {
		t.Errorf("the chart switch reads %q", labels)
	}

	// And the shape survived the language change rather than falling back to the
	// default, because the redraw goes through the same state the buttons write.
	if shape := p.chartShape(); shape != "path" && shape != "circle" {
		t.Errorf("switching language redrew the chart as %q, losing the chosen shape",
			shape)
	}
}

// chartShape names the element the evaluation's chart is currently drawn with.
//
// Waited for rather than asked once. The chart is drawn from a second request -
// the evaluation's table arrives first and the figures behind the picture after
// it - so a case that reads the shape the moment the result appears is reading an
// empty box and reporting it as a chart that was never drawn.
func (p *page) chartShape() string {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for {
		shape := p.currentChartShape()
		if shape != "nothing" || time.Now().After(deadline) {
			return shape
		}

		time.Sleep(150 * time.Millisecond)
	}
}

func (p *page) currentChartShape() string {
	p.t.Helper()

	var shape string

	p.run("read the chart's shape", chromedp.Evaluate(
		`(() => {
			const svg = document.querySelector('#report-chart svg');
			if (!svg) return 'nothing';

			for (const name of ['path', 'circle', 'rect']) {
				if (svg.querySelector(name)) return name;
			}

			return 'unknown';
		})()`, &shape))

	return shape
}

// Two projects are drawn in two colours, and each keeps its own.
//
// One accent for every bar meant a chart of five projects was five identical
// bars of different lengths: the labels carried all of it, and the eye had
// nothing to group by.
func TestEachProjectKeepsItsOwnColourInTheCharts(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	// An hour on each, so both are in the chart.
	for _, project := range []string{"Dachsanierung", "Serverumzug"} {
		p.bookAnHourOn(t, project)
	}

	p.run("open the hours card", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#statistics-card", chromedp.ByID))

	p.run("evaluate", p.click("#statistics-load"))
	p.waitForNode("#chart-projects .chart-bar")

	var fills []string

	p.evalJSON(`JSON.stringify(
		[...document.querySelectorAll('#chart-projects .chart-bar')]
			.map((bar) => getComputedStyle(bar).fill))`, &fills)

	if len(fills) < 2 {
		t.Fatalf("the chart drew %d bars, so there is nothing to tell apart", len(fills))
	}

	if fills[0] == fills[1] {
		t.Errorf("both projects are drawn in %s", fills[0])
	}
}

// Every screen that evaluates a period says which period, before it is asked to.
//
// They disagreed: the statistics card filled its fields in when Evaluate was
// pressed - so the answer arrived for a period nobody had seen until the fields
// changed under it - and the report and overtime forms sent nothing at all,
// quietly evaluating the whole history.
func TestTheEvaluationScreensArriveWithTheirPeriodFilledIn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	for _, screen := range []struct {
		view, from, to string
	}{
		{"overtime", "#statistics-from", "#statistics-to"},
		{"overtime", `#form-overtime input[name="from"]`, `#form-overtime input[name="to"]`},
		{"report", `#form-report input[name="from"]`, `#form-report input[name="to"]`},
	} {
		t.Run(screen.view+" "+screen.from, func(t *testing.T) {
			p.run("open "+screen.view, p.click(`.tab[data-view="`+screen.view+`"]`),
				chromedp.WaitVisible(screen.from, chromedp.ByQuery))

			from := p.value(screen.from)
			to := p.value(screen.to)

			if from == "" || to == "" {
				t.Fatalf("the period reads %q to %q before anything was pressed",
					from, to)
			}

			// The month it is, which is the period somebody almost always wants
			// and the only one that can be checked at a glance.
			if !strings.HasSuffix(from, "-01") {
				t.Errorf("the period starts at %q rather than at the first of a month", from)
			}

			if from > to {
				t.Errorf("the period runs from %q to %q, which is backwards", from, to)
			}
		})
	}
}
