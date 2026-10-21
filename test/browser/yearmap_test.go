//go:build browser

package browser

import (
	"fmt"
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

// The year fits a tablet's window, and scrolls only on a telephone.
//
// A tile was twelve pixels at every width, so the year's fifty-three weeks came
// to 742 pixels and an iPad held upright gives the card 690 - the last weeks of
// December lay past its edge on a screen with no scroll bar to say so. A
// telephone scrolls the year sideways, as the card says it should rather than
// squeeze it; from a tablet's width on the whole year is in view. Asked at 768
// with a desktop's scroll bar as well as a tablet's overlay one.
func TestTheYearFitsATablet(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open the overtime", p.click(`.tab[data-view="overtime"]`),
		chromedp.WaitVisible("#year-card", chromedp.ByID))
	p.waitForNode("#year-tiles .year-tile")

	for _, size := range []struct {
		w, h   int64
		mobile bool
	}{{768, 1024, true}, {768, 1024, false}, {820, 1180, true}, {1024, 768, true}} {
		opts := []chromedp.EmulateViewportOption{}
		if size.mobile {
			opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch)
		}

		p.run(fmt.Sprintf("%dx%d", size.w, size.h), chromedp.EmulateViewport(size.w, size.h, opts...))

		var fit struct {
			Wide float64 `json:"wide"`
			Room float64 `json:"room"`
		}

		p.evalJSON(`(() => {
			const wrap = document.querySelector('#year-card .year-wrap');
			return JSON.stringify({wide: wrap.scrollWidth, room: wrap.clientWidth});
		})()`, &fit)

		if fit.Wide > fit.Room+1 {
			t.Errorf("at %dx%d (mobile: %v) the year is %.0fpx wide in %.0fpx, so December is out of view",
				size.w, size.h, size.mobile, fit.Wide, fit.Room)
		}
	}
}
