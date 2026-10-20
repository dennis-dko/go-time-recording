//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The year is drawn a tile a day: today as dark as its six hours make it, the
// state's public holidays and the days away outlined, and the same year month by
// month as figures beside the year before.
func TestTheYearIsDrawnATileADay(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var daysInYear int

	p.awaitJSON(`(async () => {
		const year = Number(todayISO().slice(0, 4));
		await api('/me/holidays', { method: 'PUT', body: JSON.stringify({ region: 'BY' }) });
		await api('/timesheets', { method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 6 }) });
		const monday = new Date(Date.UTC(year, 2, 1));
		while (monday.getUTCDay() !== 1) monday.setUTCDate(monday.getUTCDate() + 1);
		const day = monday.toISOString().slice(0, 10);
		await api('/me/absences', { method: 'POST', body: JSON.stringify({ from: day, to: day, kind: 'sick' }) });
		return (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0 ? 366 : 365;
	})()`, &daysInYear)

	p.run("open the evaluations", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#year-card", chromedp.ByID))

	p.waitFor("a tile for every day of the year", func() bool {
		return p.count("#year-tiles .year-tile:not(.outside)") == daysInYear
	})

	if p.count("#year-tiles .year-tile.today.level-3") != 1 {
		t.Error("today's six hours are not drawn at the third step")
	}

	if p.count("#year-tiles .year-tile.holiday") < 9 {
		t.Errorf("%d holidays are outlined, want at least the nine every state keeps",
			p.count("#year-tiles .year-tile.holiday"))
	}

	if p.count("#year-tiles .year-tile.away") != 1 {
		t.Errorf("%d days are outlined as away, want the one sick day", p.count("#year-tiles .year-tile.away"))
	}

	p.waitForRows("#table-year tbody tr", 12)

	if summary := p.text("#year-summary"); !strings.Contains(summary, "Booked:") || !strings.Contains(summary, "to the same day") {
		t.Errorf("the year is summed up as %q, want what was booked and the year before to the same day", summary)
	}
}
