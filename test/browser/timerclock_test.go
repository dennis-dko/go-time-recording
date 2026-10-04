//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The stopwatch shows the time the server is measuring, on a device whose clock
// is not the server's.
//
// It counted the device's clock against the start time the server recorded. On
// a device two minutes slow the difference came out negative, was held at zero,
// and the stopwatch read 00:00:00 for the first two minutes after Start - which
// looks like a stopwatch that did not start. The server sends what it has
// measured so far beside the start, and its comment says that is what lets a
// browser with a wrong clock show the right duration; nothing read it.
func TestTheStopwatchCountsOnTheServersClock(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("set the device's clock two minutes slow", chromedp.Evaluate(`(() => {
		const actual = Date.now;
		Date.now = () => actual() - 120000;
		return 1;
	})()`, nil))

	p.run("start the stopwatch", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-start", chromedp.ByID),
		p.click("#timer-start"),
		chromedp.WaitVisible("#timer-stop", chromedp.ByID))

	time.Sleep(2500 * time.Millisecond)

	shown := p.text("#timer-elapsed")

	if shown == "" || shown == "00:00:00" {
		t.Errorf("two and a half seconds after Start the stopwatch reads %q", shown)
	}
}
