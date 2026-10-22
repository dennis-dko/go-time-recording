//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The last working day with nothing booked on it stands in a banner whose link
// opens the booking form on that day, and the banner goes once the day is booked.
// The day is worked out here the way the server does it, from today in the
// account's own zone and back past a weekend: a fresh installation keeps no public
// holidays until a state is chosen.
func TestTheLastUnbookedWorkingDayLeadsToTheBookingForm(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var day struct {
		ISO   string `json:"iso"`
		Shown string `json:"shown"`
	}

	p.awaitJSON(`(async () => {
		const at = new Date(todayISO() + 'T00:00:00Z');
		at.setUTCDate(at.getUTCDate() - 1);
		while ([0, 6].includes(at.getUTCDay())) at.setUTCDate(at.getUTCDate() - 1);

		const iso = at.toISOString().slice(0, 10);
		const earlier = new Date(at);
		earlier.setUTCDate(earlier.getUTCDate() - 7);

		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: earlier.toISOString().slice(0, 10), durationHours: 8 }),
		});
		await loadNotices();

		return { iso, shown: fmtDate(iso) };
	})()`, &day)

	p.waitShown("#unbooked-banner")

	if said := p.text("#unbooked-banner"); !strings.Contains(said, day.Shown) ||
		!strings.Contains(said, "a working day by your plan") {
		t.Errorf("the banner says %q, want it to name %s as a working day", said, day.Shown)
	}

	p.run("follow the banner", p.click("#unbooked-banner-open"),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	if view := p.currentView(); view != "timesheets" {
		t.Errorf("the banner's link opened %q, want the time entries", view)
	}

	if filled := p.value(`#form-timesheet input[name="date"]`); filled != day.ISO {
		t.Errorf("the booking form holds %q, want the unbooked day %s", filled, day.ISO)
	}

	var booked bool

	p.awaitJSON(`(async () => {
		await api('/timesheets', { method: 'POST', body: JSON.stringify({ date: '`+day.ISO+`', durationHours: 8 }) });
		await loadNotices();
		return true;
	})()`, &booked)

	p.waitGone("#unbooked-banner")
}

// A stopwatch past the hours its owner chose is mentioned in a banner, and once in
// a notification when it crosses them while the page is open - here by the choice
// being made while the clock has already run two hours, which is a crossing like
// any other. A clock found already past them on loading is mentioned by the banner
// alone. The browser's notifications are stood in for, so what was asked of them
// can be read back.
func TestAStopwatchPastTheChosenHoursIsMentionedOnce(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var ready bool

	p.awaitJSON(`(async () => {
		window.notified = [];
		window.Notification = class {
			static permission = 'granted';
			static requestPermission() { return Promise.resolve('granted'); }
			constructor(title, options) { window.notified.push({ title, body: options.body }); }
		};

		await api('/me/reminders', { method: 'PUT', body: JSON.stringify({ unbookedDay: true, timerAlertHours: 0 }) });
		await loadMe();
		await api('/me/timer', { method: 'POST', body: '{}' });
		const startedAt = new Date(Date.now() - 2 * 3600e3).toISOString().replace(/\.\d+Z$/, 'Z');
		await api('/me/timer', { method: 'PUT', body: JSON.stringify({ startedAt }) });
		await loadTimer();
		return true;
	})()`, &ready)

	if p.visible("#timer-banner") {
		t.Fatal("the banner stands while the stopwatch is never to be mentioned")
	}

	p.awaitJSON(`(async () => {
		await api('/me/reminders', { method: 'PUT', body: JSON.stringify({ unbookedDay: true, timerAlertHours: 1 }) });
		await loadMe();
		return true;
	})()`, &ready)

	p.waitShown("#timer-banner")

	if said := p.text("#timer-banner"); !strings.Contains(said, "longer than the 1 h you chose to be told after") {
		t.Errorf("the banner says %q", said)
	}

	var notified []struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}

	p.evalJSON(`JSON.stringify(window.notified)`, &notified)

	if len(notified) != 1 || notified[0].Title != "The stopwatch is still running" ||
		!strings.Contains(notified[0].Body, "longer than the 1 h you chose") {
		t.Errorf("the browser was asked to show %+v, want one notification about the stopwatch", notified)
	}

	// Loaded again, the clock is found past the hours on its first look: the
	// banner stays, and nobody is notified a second time about the same clock.
	p.awaitJSON(`(async () => { await loadTimer(); return true; })()`, &ready)
	p.evalJSON(`JSON.stringify(window.notified)`, &notified)

	if len(notified) != 1 || !p.visible("#timer-banner") {
		t.Errorf("after loading the clock again %d notification(s) were asked for and the banner shows: %v, "+
			"want still one and the banner", len(notified), p.visible("#timer-banner"))
	}
}
