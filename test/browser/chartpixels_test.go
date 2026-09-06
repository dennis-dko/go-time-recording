//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/dennis-dko/go-time-recording/internal/pkg/document"
)

// The picture of a chart stays inside the bound the server decodes it with.
//
// document.MaxChartPixels refuses anything larger, and the reasoning written
// above it is about width: "even a chart running the full width of a 5120-pixel
// screen is around 8 megapixels". That holds for the width, which is the card's,
// and not for the height, which is the number of rows: drawBarChart gives each
// bar 26 pixels and the picture is taken at CHART_SCALE, so the height grows with
// the period being evaluated and nothing bounds it.
//
// An evaluation of one project over a year is 365 rows. Nothing in the form
// limits the period - there is no maximum on either date - so this is an ordinary
// request rather than an extreme one, and what it produced was a document the
// server would refuse with no way for the reader to know why except to shorten
// the period and guess.
//
// Driven at the drawing and the picture directly: both are pure functions of what
// they are handed, and this is a question about size.
func TestAYearOfDaysStillFitsThePictureBound(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var out string

	p.run("draw a year and take its picture", chromedp.Evaluate(`(async () => {
		const box = document.createElement('div');
		document.body.append(box);

		const bars = Array.from({ length: 365 }, (_, i) => ({
			label: 'Day ' + i, value: (i % 8) + 1,
		}));

		drawBarChart(box, bars, (v) => v + ' h');

		const uri = await chartAsPicture(box);

		const picture = new Image();
		await new Promise((resolve, reject) => {
			picture.onload = resolve;
			picture.onerror = () => reject(new Error('the picture did not decode'));
			picture.src = uri;
		});

		const answer = {
			width: picture.naturalWidth,
			height: picture.naturalHeight,
			pixels: picture.naturalWidth * picture.naturalHeight,
			bytes: uri.length,
		};

		box.remove();

		return JSON.stringify(answer);
	})()`, &out, awaitPromise))

	var picture struct {
		Width  int `json:"width"`
		Height int `json:"height"`
		Pixels int `json:"pixels"`
		Bytes  int `json:"bytes"`
	}

	if err := json.Unmarshal([]byte(out), &picture); err != nil {
		t.Fatalf("reading the picture (%q): %v", out, err)
	}

	t.Logf("365 days came out as %dx%d = %d pixels, %d bytes of data URI",
		picture.Width, picture.Height, picture.Pixels, picture.Bytes)

	if picture.Pixels == 0 {
		t.Fatal("the picture has no size, so this case is not measuring anything")
	}

	if picture.Pixels > document.MaxChartPixels {
		t.Errorf("a year of days comes out at %d pixels, and the server refuses "+
			"anything over %d. The bound is reasoned about the width, which is the "+
			"card's; the height is the number of rows, and nothing bounds that",
			picture.Pixels, document.MaxChartPixels)
	}
}
