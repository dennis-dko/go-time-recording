//go:build browser

package browser

import (
	"os"
	"testing"

	"github.com/chromedp/chromedp"
)

// The live log's controls stack on a telephone without a gap opening between
// them.
//
// The search box shares a row with the refresh interval by a flex basis of
// 200px. Below 640px the controls are laid out as a column instead, and a basis
// is measured along whichever way the line runs: there the search box was 200px
// tall, its input at the top and the rest empty - the gap a screenshot from a
// telephone showed between "Search" and "Refresh every (s)".
func TestTheLogControlsStackWithoutAGapOnATelephone(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Opened wide, where the tab is on the bar rather than behind the menu, and
	// then narrowed: the measurement is of the layout, not of how one gets there.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))
	p.run("a telephone", chromedp.EmulateViewport(390, 844))

	var gap float64

	p.run("measure", chromedp.Evaluate(`(() => {
		const search = document.querySelector('#log-search').getBoundingClientRect();
		const delay = document.querySelector('label.log-delay').getBoundingClientRect();
		return delay.top - search.bottom;
	})()`, &gap))

	// The controls' own gap between rows is 12px.
	if gap > 24 {
		t.Errorf("on a 390px screen the search box ends %.0fpx above the refresh interval; "+
			"the controls are 12px apart everywhere else", gap)
	}

	var picture []byte

	p.run("the controls as drawn", chromedp.Screenshot(".log-controls", &picture, chromedp.ByQuery))

	if err := os.WriteFile("logcontrols.png", picture, 0o600); err != nil {
		t.Logf("the picture of the controls could not be saved: %v", err)
	}
}
