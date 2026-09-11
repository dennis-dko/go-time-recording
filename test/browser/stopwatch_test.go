//go:build browser

package browser

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The clock counts up in the page without asking the server every second, and
// stopping it books what was measured. Both halves are browser behaviour: the
// ticking display, and the buttons swapping over when it starts.
func TestTheStopwatchRunsAndBooksWhatItMeasured(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open time entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID))

	if !p.visible("#timer-start") {
		t.Fatal("no start button")
	}

	p.run("start it",
		chromedp.SendKeys("#timer-description", "measured in a browser", chromedp.ByID),
		p.click("#timer-start"))

	// The buttons swap over, which is how somebody can tell it took.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) && !p.visible("#timer-stop") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-stop") {
		t.Fatal("the stop button never appeared, so the clock did not start")
	}

	if p.visible("#timer-start") {
		t.Error("the start button is still offered while the clock runs")
	}

	// And it counts up on its own. Two readings a couple of seconds apart have to
	// differ, or the display is a decoration.
	first := p.text("#timer-elapsed")
	if first == "" {
		t.Fatal("the elapsed display is empty while the clock runs")
	}

	if second := p.waitChanged("#timer-elapsed", first); second == first {
		t.Errorf("the display is stuck at %q, so it is not counting", first)
	}

	// Past the smallest bookable duration - below it the stop is refused on
	// purpose, which is a different test.
	time.Sleep(38 * time.Second)

	p.run("stop and book", p.click("#timer-stop"))

	// The entry appears in the list, with the description that was typed.
	p.waitForText("#table-timesheets tbody", "measured in a browser")

	// And the clock is back to offering a start.
	deadline = time.Now().Add(waitPatience)
	for time.Now().Before(deadline) && !p.visible("#timer-start") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-start") {
		t.Error("after stopping, the clock does not offer to start again")
	}
}
