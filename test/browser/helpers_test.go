//go:build browser

package browser

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "…"
}

// waitForFilled waits until an element has something written in it.
//
// For the screens whose cards are in the markup and whose contents arrive by
// request: the card is there from the first paint, and only what is in it says
// whether the answer has come back.
func (p *page) waitForFilled(selector string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text(selector)) != "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s was never written to, so the screen stopped loading before it;"+
		" the log says:\n%s", selector, p.app.Log())
}

// waitForValue waits until a field has something in it.
//
// For the screens whose forms are in the markup and whose contents arrive by
// request: the field is there from the first paint, and only what is in it says
// whether the answer has come back.
func (p *page) waitForValue(selector string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if p.value(selector) != "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s was never filled in, so the screen stopped loading before it;"+
		" the log says:\n%s", selector, p.app.Log())
}

// bookAnHourOn creates a project and books an hour against it, through the API.
//
// Through the API rather than through the forms: what is being examined here is
// the chart, and eight form steps per project would put the whole of project
// creation and time entry between the test and the thing it is about.
func (p *page) bookAnHourOn(t *testing.T, name string) {
	t.Helper()

	var status string

	p.run("book an hour on "+name, chromedp.Evaluate(fmt.Sprintf(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const headers = { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf };

			// Today in this browser's own zone, which is what the application means
			// by today. toISOString() is UTC, and between local midnight and UTC
			// midnight the two are different days - so east of Greenwich this booked
			// yesterday, the statistics card evaluated the default period of "the
			// first of this month to today", and the entry was outside it. The chart
			// then drew nothing, for two hours a night, on a machine in Berlin.
			const now = new Date();
			const today = now.getFullYear() + '-'
				+ String(now.getMonth() + 1).padStart(2, '0') + '-'
				+ String(now.getDate()).padStart(2, '0');

			const made = await fetch('/api/v1/projects', {
				method: 'POST',
				credentials: 'same-origin',
				headers,
				body: JSON.stringify({ name: %q, startDate: today }),
			});

			if (!made.ok) return 'project ' + made.status;

			const project = (await made.json())?.data;

			const booked = await fetch('/api/v1/timesheets', {
				method: 'POST',
				credentials: 'same-origin',
				headers,
				body: JSON.stringify({
					projectId: project.id,
					date: today,
					durationHours: 1,
				}),
			});

			return booked.ok ? 'ok' : 'entry ' + booked.status;
		})()`, name), &status, awaitPromise))

	if status != "ok" {
		t.Fatalf("booking an hour on %s: %s", name, status)
	}
}

// waitForNode waits until something matches.
func (p *page) waitForNode(selector string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if p.count(selector) > 0 {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.t.Fatalf("nothing ever matched %s", selector)
}

// submitAndAwaitReload sends the appearance form and waits for the page to come
// back.
//
// A changed mark reloads: no engine takes a new tab icon from a link swapped in
// afterwards, and a differently cropped logo is a different icon. The reload
// takes the saved notice away with it, so waiting for that notice is waiting for
// something that is being removed - which is a wait that sometimes sees it and
// sometimes does not, depending on how quickly the request came back.
//
// The reload itself is what is waited for, by a mark left on the document that
// is about to be replaced.
func (p *page) submitAndAwaitReload(t *testing.T, submit chromedp.Action) {
	t.Helper()

	p.run("mark this document", chromedp.Evaluate(`window.__beforeReload = true`, nil))
	p.run("save", submit)

	deadline := time.Now().Add(waitPatience)

	for {
		var gone bool

		p.run("wait for the reload", chromedp.Evaluate(
			`window.__beforeReload === undefined`, &gone))

		if gone {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("the save never reloaded the page; the notice says %q",
				p.text("#toast"))
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.run("wait for the screen", chromedp.WaitVisible("#form-branding", chromedp.ByID))
}

// readyWorker signs in as somebody who works here.
//
// The built-in administrator records no time: it exists on every installation before
// anybody has chosen anything, so it is how you get in rather than somebody's working
// day. A case about booking, a calendar, a stopwatch, a chart or a project therefore
// cannot be driven by it - every one of those screens is gated on a right it does not
// hold, and the case would be testing an empty page.
//
// The administrator is still needed first: the initial password has to be replaced
// before anything else answers, and only it can create an account.
// thisMonth is the statistics range these cases evaluate, and how many days it
// holds.
//
// Derived rather than written down. It used to be the literal 2026-08-01 to
// 2026-08-31, which was the current month on the day it was written and stopped
// containing anything at all the moment September began: the entry these cases
// book carries the booking form's default, which is today, so from the first of
// the next month the chart was a month of empty days.
//
// One of the three said so - TestTheOwnHoursChartsAreDrawn failed with every bar
// at 0.00 h. The other two failed quietly, exporting a picture of nothing while
// their own comments observed that an empty chart would prove very little. A test
// that goes on passing while it stops testing anything is the worse half of this.
//
// A calendar month rather than a rolling window, because the chart draws one row
// per day of a month and the row count is asserted against this.
func thisMonth() (from, to string, days int) {
	now := time.Now()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)

	return first.Format(time.DateOnly), last.Format(time.DateOnly), last.Day()
}
