package web_test

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/document"
)

// The bound the page draws to is the bound the server decodes with.
//
// app.js scales a chart's picture down to fit MAX_PICTURE_PIXELS before posting
// it, and the server refuses anything over document.MaxChartPixels. Two numbers
// for one rule: if the page's is the larger, the document is refused with a
// message about pixels, which says nothing to somebody who asked for a year of
// days; if it is much smaller, charts are blurred for no reason.
//
// A year of days was measured at 48 megapixels before the scaling existed, so
// this is not a theoretical pairing.
func TestTheChartPictureBoundIsTheOneTheServerEnforces(t *testing.T) {
	js := asset(t, "/app.js")

	match := regexp.MustCompile(`MAX_PICTURE_PIXELS = ([0-9 *]+);`).FindStringSubmatch(js)
	if match == nil {
		t.Fatal("app.js no longer declares MAX_PICTURE_PIXELS; the picture it posts " +
			"is no longer bounded by anything this test can read")
	}

	drawn := 1

	for _, factor := range strings.Split(match[1], "*") {
		value, err := strconv.Atoi(strings.TrimSpace(factor))
		if err != nil {
			t.Fatalf("MAX_PICTURE_PIXELS reads %q, which this test cannot work out: %v",
				match[1], err)
		}

		drawn *= value
	}

	if drawn == document.MaxChartPixels {
		return
	}

	t.Errorf("the page bounds its picture at %d pixels and the server decodes up "+
		"to %d. A page bound above the server's means a document refused with a "+
		"message about pixels; one far below means charts blurred for nothing",
		drawn, document.MaxChartPixels)
}

// The interface reads the field names the server actually sends.
//
// This is not a translation check, but it lives with them because it is the same
// class of mistake: a name that has to match something outside the file, written
// from memory. The evaluation's charts read project.project and day.booked -
// neither of which exists - so every label came out as "no project" and the
// per-day value was undefined, which threw inside the formatter and left an
// empty chart behind an error toast. Nothing failed loudly, and no test noticed.
func TestTheChartsReadFieldsTheStatisticsEndpointSends(t *testing.T) {
	js := asset(t, "/app.js")

	// The names on the wire, taken from the response type rather than repeated
	// here: a rename there has to show up as a failure here.
	source := readSource(t, filepath.Join("..", "api", "v1", "rest", "statistics_handler.go"))

	for _, field := range []string{"date", "hours", "name", "projectId"} {
		if !strings.Contains(source, `json:"`+field+`"`) {
			t.Fatalf("statistics_handler.go no longer sends %q; this test is out of date", field)
		}
	}

	// Comments stripped first, and word boundaries on the names. The prose above
	// the fixed code names the wrong fields on purpose, and project.projectId is
	// a real field that starts with one of them - both would otherwise read as
	// the bug they describe.
	code := withoutLineComments(js)

	for _, wrong := range []string{"day.booked", "project.project", "day.total", "project.total"} {
		if regexp.MustCompile(regexp.QuoteMeta(wrong) + `\b`).MatchString(code) {
			t.Errorf("a chart reads %s, which the statistics endpoint does not send", wrong)
		}
	}

	// And the two the report chart depends on, so a rename cannot quietly break
	// only the screen no Go test covers.
	for _, needed := range []string{"day.hours", "project.hours", "project.name"} {
		if !strings.Contains(js, needed) {
			t.Errorf("no chart reads %s any more; the report chart needs it", needed)
		}
	}
}
