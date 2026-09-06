package web_test

import (
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
