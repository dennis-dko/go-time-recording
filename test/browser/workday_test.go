//go:build browser

package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// A day's working time is recorded on the calendar screen and read back beside
// the hours booked on it: the form offers the running stopwatch's start as the
// day's, the table shows what was worked and what was booked, and the month
// leaves as a document.
func TestAWorkingDayIsRecordedBesideTheHoursBookedOnIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var clockStart string

	p.awaitJSON(`(async () => {
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: todayISO(), durationHours: 3 }),
		});
		await api('/me/timer', { method: 'POST', body: JSON.stringify({}) });
		await loadTimer();
		return wallClock(runningTimer.startedAt).time;
	})()`, &clockStart)

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#workday-card", chromedp.ByID))

	p.waitForValue(`#form-workday input[name="start"]`)

	if offered := p.value(`#form-workday input[name="start"]`); offered != clockStart {
		t.Errorf("the form offers %q as the day's start, want the stopwatch's %q", offered, clockStart)
	}

	p.run("record the day",
		chromedp.SetValue(`#form-workday input[name="start"]`, "08:00", chromedp.ByQuery),
		chromedp.SetValue(`#form-workday input[name="end"]`, "16:30", chromedp.ByQuery),
		chromedp.SetValue(`#form-workday input[name="breakMinutes"]`, "30", chromedp.ByQuery),
		p.click(`#form-workday button[type="submit"]`))

	p.waitForText("#table-workdays tbody", "08:00")

	row := p.text("#table-workdays tbody")
	for _, want := range []string{"16:30", "30", "8.00 h", "3.00 h"} {
		if !strings.Contains(row, want) {
			t.Errorf("the day reads %q, want %q in it", row, want)
		}
	}

	p.waitForText("#workday-total", "Worked 8.00 h, booked 3.00 h")

	into := t.TempDir()

	p.run("take downloads here", browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).
		WithDownloadPath(filepath.FromSlash(into)).WithEventsEnabled(true))

	p.run("export the month", p.click("#workday-pdf"))

	document, err := os.ReadFile(waitForDownload(t, into, ".pdf"))
	if err != nil {
		t.Fatalf("reading what was saved: %v", err)
	}

	if !strings.HasPrefix(string(document), "%PDF-") {
		t.Errorf("what was saved is not a PDF; it starts %q", truncateText(string(document), 40))
	}

	// And nothing of it stays for whoever signs in next.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var left string

	p.run("read what the next account would find", chromedp.Evaluate(
		`document.querySelector('#workday-total').textContent + document.querySelector('#table-workdays tbody').textContent`,
		&left))

	if strings.TrimSpace(left) != "" {
		t.Errorf("after the sign-out the card still says %q", left)
	}
}
