//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The activities written into descriptions with a # are drawn beside the
// projects, each by its tag, and the hours naming none as a bar of their own.
func TestTheActivitiesAreDrawnBesideTheProjects(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.awaitJSON(`(async () => {
		for (const [description, durationHours] of [['Planning #meeting', 1.5], ['Parser #review', 3], ['Quiet work', 2]]) {
			await api('/timesheets', {
				method: 'POST', body: JSON.stringify({ date: todayISO(), description, durationHours }),
			});
		}
		return true;
	})()`, nil)

	p.run("open my hours", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#statistics-card", chromedp.ByID),
		p.click("#statistics-load"))

	p.waitForText("#chart-activities", "#review")
	p.waitForText("#chart-activities", "#meeting")
	p.waitForText("#chart-activities", "No activity")
}
