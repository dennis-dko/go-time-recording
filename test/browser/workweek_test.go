//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A week is planned from My account: the form opens on the week in force, in the
// calendar's own weekday names, and a saved plan is listed and read back by the
// server as it was typed.
func TestAWeekIsPlannedFromMyAccount(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("open My account", p.click(`.tab[data-view="settings"]`),
		chromedp.WaitVisible("#form-schedule", chromedp.ByID))

	p.waitForValue(`#form-schedule input[name="day0"]`)

	if names := p.text("#schedule-days"); !strings.Contains(names, "Mon") || !strings.Contains(names, "Sun") {
		t.Errorf("the plan's days are named %q, want the calendar's weekday names", names)
	}

	var week []string

	p.awaitJSON(`Array.from({ length: 7 }, (_, i) =>
		document.querySelector('#form-schedule input[name="day' + i + '"]').value)`, &week)

	if strings.Join(week, ",") != "8,8,8,8,8,0,0" {
		t.Errorf("the form opens on %v, want the default week", week)
	}

	// A six-hour Friday from the 3rd of August.
	p.run("plan a shorter Friday", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-schedule');
		form.elements.day4.value = '6';
		setDateField(form.elements.validFrom, '2026-08-03');
	})()`, nil),
		p.click(`#form-schedule button[type="submit"]`))

	var saved struct {
		Items []struct {
			ValidFrom string    `json:"validFrom"`
			Hours     []float64 `json:"hours"`
		} `json:"items"`
	}

	// The server's answer rather than the table's rows: an empty table has a row
	// of its own saying so, and a count of one would be met before the save is.
	deadline := time.Now().Add(waitPatience)

	for len(saved.Items) == 0 && time.Now().Before(deadline) {
		p.awaitJSON(`api('/me/schedules')`, &saved)

		if len(saved.Items) == 0 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// And the list shows it, once the reload behind the save has drawn it.
	p.waitForText("#table-schedules tbody", "2026")

	if len(saved.Items) != 1 || saved.Items[0].ValidFrom != "2026-08-03" || saved.Items[0].Hours[4] != 6 {
		t.Errorf("the server holds %+v, want the six-hour Friday from the 3rd of August", saved.Items)
	}
}
