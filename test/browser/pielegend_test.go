//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// The pie's key stays inside the picture however many projects there are.
//
// The key is not decoration on this chart, and chartColourFor says so where it
// explains why eight hues are enough for any number of projects: "the label is
// beside every bar and in the pie's key, so the colour groups the eye rather than
// carrying the meaning on its own." Two projects sharing a hue is only acceptable
// while the key is there to tell them apart.
//
// The key is drawn down the right at y = 14 + index * 20 into a viewBox 260 high,
// so the thirteenth entry lands past the bottom edge and every one after it with
// it. Nothing clips visibly - the SVG simply ends - so the chart looks finished
// and is missing exactly the part that carries the meaning. The bar chart grows
// its height with the number of bars, and the column chart says in its own
// comment that it runs out of room at about a dozen; the pie said nothing and
// looked fine.
//
// Driven at the drawing function rather than through thirteen real projects: it
// is a pure function of the slices it is handed, and this is about geometry.
func TestThePieKeyFitsInThePicture(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var out string

	p.run("draw a pie with more projects than the box was built for",
		chromedp.Evaluate(`(() => {
			const box = document.createElement('div');
			document.body.append(box);

			const slices = Array.from({ length: 14 }, (_, i) => ({
				label: 'Project ' + i, value: i + 1, key: 'project-' + i,
			}));

			drawPieChart(box, slices, (v) => v + ' h');

			const chart = box.querySelector('svg');
			const labels = [...box.querySelectorAll('text.chart-label')];
			const lowest = labels.reduce(
				(low, node) => Math.max(low, Number(node.getAttribute('y'))), 0);

			const answer = {
				labels: labels.length,
				lowest,
				boxHeight: Number(chart.getAttribute('viewBox').split(' ')[3]),
				height: Number(chart.getAttribute('height')),
			};

			box.remove();

			return JSON.stringify(answer);
		})()`, &out))

	var drawn struct {
		Labels    int     `json:"labels"`
		Lowest    float64 `json:"lowest"`
		BoxHeight float64 `json:"boxHeight"`
		Height    float64 `json:"height"`
	}

	if err := json.Unmarshal([]byte(out), &drawn); err != nil {
		t.Fatalf("reading what was drawn (%q): %v", out, err)
	}

	if drawn.Labels != 14 {
		t.Fatalf("the pie drew %d key entries for 14 slices, so this case is not "+
			"measuring what it thinks it is", drawn.Labels)
	}

	if drawn.Lowest > drawn.BoxHeight {
		t.Errorf("the last key entry sits at y=%v in a viewBox %v high, so it and "+
			"everything past the twelfth is outside the picture. The key is what "+
			"tells two projects sharing a hue apart, which is the reason "+
			"chartColourFor gives for eight hues being enough",
			drawn.Lowest, drawn.BoxHeight)
	}

	if drawn.Height != drawn.BoxHeight {
		t.Errorf("the height attribute says %v and the viewBox says %v; the drawing "+
			"is scaled to a box of the wrong shape", drawn.Height, drawn.BoxHeight)
	}
}
