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
