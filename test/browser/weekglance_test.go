//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The greeting shows this week: seven days in the calendar's names, today's hours
// against today's target with the running clock counted in, and the week's total.
func TestTheGreetingShowsThisWeek(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Two hours booked today, and a clock started an hour ago: today should read
	// three, because the clock is time being worked.
	// Less in the hour after midnight, when an hour ago was yesterday and the
	// clock belongs to that day - worked out where the page decides it.
	var expected string

	p.awaitJSON(`(async () => {
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 2 }),
		});
		await api('/me/timer', { method: 'POST', body: '{}' });
		const startedAt = new Date(Date.now() - 3600e3).toISOString().replace(/\.\d+Z$/, 'Z');
		await api('/me/timer', { method: 'PUT', body: JSON.stringify({ startedAt }) });
		return wallClock(startedAt).date === todayISO() ? '3.00' : '2.00';
	})()`, &expected)

	p.run("open the greeting", p.click("#app-title"),
		chromedp.WaitVisible("#welcome-week", chromedp.ByID))

	p.waitForRows("#welcome-week-bars li", 7)

	p.waitForText("#welcome-week-today", expected)

	if today := p.text("#welcome-week-today"); !strings.Contains(today, "of") {
		t.Errorf("today reads %q, want its hours against its target", today)
	}

	if total := p.text("#welcome-week-total"); !strings.Contains(total, expected) {
		t.Errorf("the week reads %q, want today's %s hours in it", total, expected)
	}

	if names := p.text("#welcome-week-bars"); !strings.Contains(names, "Mon") || !strings.Contains(names, "Sun") {
		t.Errorf("the days are named %q, want the calendar's weekday names", names)
	}
}
