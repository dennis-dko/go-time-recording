//go:build browser

package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// An evaluation leaves the screen as a document, with the chart that was on it.
//
// This is the one part of the export that only a real browser can answer.
// Everything else - the layout, the page breaks, the refusal of a chart that is
// not a picture - is settled by the Go tests beside the writer. What cannot be
// settled there is whether a chart drawn in the page can be turned into a
// picture at all, and that is a question about three things a browser decides:
//
//   - whether an <svg> lifted out of the document still has its colours, which
//     it does not unless the settled style is written into the copy first;
//   - whether the Content-Security-Policy lets an image be loaded from a data:
//     URI, which is the only way in with no external origin allowed;
//   - and whether a canvas drawn on by that image is still readable afterwards.
//     A canvas the browser considers tainted throws on toDataURL, and it throws
//     at the moment somebody presses the button rather than when the code is
//     written.
func TestAnEvaluationLeavesAsADocumentWithItsChart(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Something to evaluate. An empty chart would be a picture of nothing, which
	// this would happily produce and prove very little with.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "3.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "3.25")

	p.run("open overtime", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#statistics-card", chromedp.ByID))

	p.run("evaluate",
		chromedp.SetValue("#statistics-from", "2026-08-01", chromedp.ByID),
		chromedp.SetValue("#statistics-to", "2026-08-31", chromedp.ByID),
		p.click("#statistics-load"))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#chart-days svg .chart-bar") {
		t.Fatalf("no chart was drawn, so there is nothing to export; the container holds %q",
			truncateText(p.text("#chart-days"), 200))
	}

	// The picture, taken by the page's own code rather than by a copy of it
	// written here - the point is to drive what the button drives.
	var picture string

	p.run("take a picture of the chart", chromedp.Evaluate(
		`chartAsPicture(document.querySelector('#chart-days'))`, &picture,
		func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}))

	if !strings.HasPrefix(picture, "data:image/png;base64,") {
		t.Fatalf("the chart did not become a PNG; it became %q",
			truncateText(picture, 80))
	}

	drawn := decodePicture(t, picture)

	// The bars are there, in the colours the stylesheet gives them.
	//
	// This is the assertion the first version of this test did not make, and the
	// version that only checked the size passed with the colours removed: a copy
	// with no settled style written into it is still a picture of the right
	// shape and size, drawn entirely in the browser's default black. The colour
	// is precisely what the stylesheet contributes and precisely what a copy
	// lifted out of the page loses.
	if !hasColour(drawn) {
		t.Error("the picture is black and white, so the chart lost its colours on " +
			"the way out of the page")
	}
}

// The chart in the document is the shape that was chosen, not a default.
//
// This is the whole of what was asked for - "the chart as selected" - and it is
// the one claim that cannot be checked by looking at the finished PDF: a picture
// of bars and a picture of a pie are both an image in a document, and nothing on
// this side can tell them apart.
//
// What can be told apart is one picture from another. Taking the picture twice,
// with the shape changed in between, is the same question asked in a way that
// has an answer: identical bytes would mean the export drew whatever it liked
// and the choice on the screen went nowhere.
func TestTheDocumentTakesTheChartThatWasChosen(t *testing.T) {
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

	asBars := p.chartPicture()

	p.run("draw it as a circle", p.click(`#report-chart-switch button[data-chart="pie"]`))

	asPie := p.chartPicture()

	if asBars == "" || asPie == "" {
		t.Fatal("one of the two shapes produced no picture at all")
	}

	if asBars == asPie {
		t.Error("the picture is the same after changing the shape, so what would go " +
			"into the document is not what is on the screen")
	}
}

// chartPicture takes the report chart's picture, the way the export does.
//
// Waits for a drawing to be there first. Changing the shape empties the
// container and fills it again, so a picture taken the instant after the press
// is a picture of nothing - which this reported as "one of the two shapes
// produced no picture", on a run where the machine was busy enough for the gap
// to be visible.
func (p *page) chartPicture() string {
	p.t.Helper()

	p.run("wait for a drawing", chromedp.WaitReady("#report-chart svg", chromedp.ByQuery))

	var picture string

	p.run("take a picture of the report chart", chromedp.Evaluate(
		`chartAsPicture(document.querySelector('#report-chart'))`, &picture,
		func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}))

	return picture
}

// decodePicture reads back the PNG a data URI carries.
func decodePicture(t *testing.T, uri string) image.Image {
	t.Helper()

	encoded, found := strings.CutPrefix(uri, "data:image/png;base64,")
	if !found {
		t.Fatalf("not a PNG data URI: %q", truncateText(uri, 60))
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the picture is not readable base64: %v", err)
	}

	drawn, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the picture is not a readable PNG: %v", err)
	}

	return drawn
}

// hasColour reports whether anything in the picture is not a shade of grey.
//
// The ground and the type are grey or near it in both themes; the bars are not.
// The gap has to be wide enough that a hint of anti-aliasing does not count as a
// colour, and narrow enough that the accent does - #2f6feb is 188 apart, and the
// dark theme's #5b8dfa is 159.
func hasColour(drawn image.Image) bool {
	bounds := drawn.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := drawn.At(x, y).RGBA()

			high := max(r, max(g, b))
			low := min(r, min(g, b))

			if (high-low)>>8 > 40 {
				return true
			}
		}
	}

	return false
}

// Pressing the button puts a PDF on the disk.
//
// The whole way through: the page reads its own table and its own chart, posts
// them, and the server hands back a document the browser saves. Asserted on the
// file rather than on the request, because a request that was sent and a file
// that arrived are different claims and only the second one is the feature.
func TestTheExportButtonSavesAPDF(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "2", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "2")

	into := t.TempDir()

	p.run("take downloads here", browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).
		WithDownloadPath(into).WithEventsEnabled(true))

	p.run("open overtime", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#statistics-card", chromedp.ByID))

	p.run("evaluate",
		chromedp.SetValue("#statistics-from", "2026-08-01", chromedp.ByID),
		chromedp.SetValue("#statistics-to", "2026-08-31", chromedp.ByID),
		p.click("#statistics-load"))

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.run("export it", p.click("#statistics-pdf"))

	saved := waitForDownload(t, into, ".pdf")

	document, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("reading what was saved: %v", err)
	}

	if !strings.HasPrefix(string(document), "%PDF-") {
		t.Fatalf("what was saved is not a PDF; it starts %q",
			truncateText(string(document), 40))
	}

	// A document with a chart in it. One with neither picture nor figures still
	// makes a valid PDF, and it would be a page of nothing.
	if len(document) < 20000 {
		t.Errorf("the document is %d bytes, which is too small to hold a chart",
			len(document))
	}
}

// waitForDownload waits for a finished file of the wanted kind to appear.
//
// Two things make this more than "wait for the directory to have something in
// it". Chrome writes a .crdownload file while it is still writing and renames it
// when it is done, so a file that exists is not a download that has finished.
// And the directory is not ours: SetDownloadBehavior sets it for the browser,
// which drops its own things there - a run of this failed on "downloads.htm",
// a Chrome component, whose contents are quite reasonably not a PDF.
//
// So it waits for the kind of file that was asked for, and says what else was
// there if it never arrives.
func waitForDownload(t *testing.T, into, kind string) string {
	t.Helper()

	deadline := time.Now().Add(waitPatience)

	var seen []string

	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(into)
		if err != nil {
			t.Fatalf("looking in the download directory: %v", err)
		}

		seen = seen[:0]

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			seen = append(seen, entry.Name())

			if strings.HasSuffix(entry.Name(), kind) {
				return filepath.Join(into, entry.Name())
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("no %s file was downloaded within %s; the directory holds %v",
		kind, waitPatience, seen)

	return ""
}
