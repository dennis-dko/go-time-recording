//go:build browser

package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The stopwatch's corrections, on screen: moving the start, and booking a forgotten
// clock up to the time the work ended. The API cases hold the rules; these hold that a person can reach
// them, in their own zone, and that a forgotten clock is said out loud on every
// screen until it is dealt with.

// serverTimer asks the server, from the page, about the running clock.
func (p *page) serverTimer() (startedAt time.Time, projectID int) {
	p.t.Helper()

	var answer struct {
		StartedAt string `json:"startedAt"`
		ProjectID int    `json:"projectId"`
	}

	p.awaitJSON(`api('/me/timer')`, &answer)

	at, err := time.Parse(time.RFC3339, answer.StartedAt)
	if err != nil {
		p.t.Fatalf("the clock's start %q is not RFC 3339: %v", answer.StartedAt, err)
	}

	return at, answer.ProjectID
}

// The clock started late is corrected from the card it runs on, and the screen
// counts from the corrected start.
func TestTheStopwatchStartIsCorrectedOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("start the clock", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID),
		p.click("#timer-start"),
		chromedp.WaitVisible("#timer-adjust-open", chromedp.ByID))

	if since := p.text("#timer-since-text"); since == "" {
		t.Error("the card does not say when the clock started")
	}

	p.run("open the correction", p.click("#timer-adjust-open"),
		chromedp.WaitVisible("#form-timer-start", chromedp.ByID))

	// It opens on the clock's own start, so a correction of a few minutes is a
	// change of a few minutes rather than a time typed from nothing.
	if p.value(`#form-timer-start input[name="time"]`) == "" {
		t.Fatal("the correction opened empty instead of on the clock's start")
	}

	// Two hours earlier, on the reader's own wall clock - what a person types.
	p.run("move the start back two hours", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-timer-start');
		const { date, time } = wallClock(new Date(Date.now() - 2 * 3600e3).toISOString());
		setDateField(form.elements.date, date);
		form.elements.time.value = time;
	})()`, nil))

	p.run("save it", p.click(`#form-timer-start button[type="submit"]`),
		chromedp.WaitNotVisible("#form-timer-start", chromedp.ByID))

	startedAt, _ := p.serverTimer()

	// A time field has minutes and no seconds, so the start is up to a minute
	// earlier than exactly two hours ago.
	if ago := time.Since(startedAt); ago < 119*time.Minute || ago > 122*time.Minute {
		t.Errorf("the server's clock started %v ago, want about two hours", ago.Round(time.Second))
	}

	p.waitForTextPrefix("#timer-elapsed", "02:")
}

// A clock left running for longer than one entry can hold is announced on every
// screen, the stopwatch offers only the way out that works, and saying when the
// work ended books it.
func TestAForgottenStopwatchIsAnnouncedAndBookedUpToItsEnd(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("go to the stopwatch", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID))

	// Thirty hours ago, through the API: a case that waits a day is not a case.
	var status string

	p.run("leave a clock running since yesterday", chromedp.Evaluate(`(async () => {
		await api('/me/timer', { method: 'POST', body: '{}' });
		const startedAt = new Date(Date.now() - 30 * 3600e3).toISOString().replace(/\.\d+Z$/, 'Z');
		await api('/me/timer', { method: 'PUT', body: JSON.stringify({ startedAt }) });
		await loadTimer();
		return 'ok';
	})()`, &status, awaitPromise))

	if status != "ok" {
		t.Fatalf("setting up the forgotten clock: %s", status)
	}

	p.run("the banner is up", chromedp.WaitVisible("#timer-banner", chromedp.ByID))

	// On another screen as well: the person most in need of it is not looking at
	// the stopwatch.
	p.run("go to the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#view-calendar", chromedp.ByID))

	if !p.visible("#timer-banner") {
		t.Error("the banner went away on another screen")
	}

	p.run("follow the banner", p.click("#timer-banner-open"),
		chromedp.WaitVisible("#form-timer-end", chromedp.ByID))

	if p.visible("#timer-stop") {
		t.Error("stopping now is still offered, which the server can only refuse")
	}

	if !p.visible("#timer-end-forgotten") {
		t.Error("the end form opened without saying why")
	}

	p.run("say the work ended eight hours after it started", chromedp.Evaluate(`(() => {
		const form = document.querySelector('#form-timer-end');
		const ended = new Date(new Date(runningTimer.startedAt).getTime() + 8 * 3600e3);
		const { date, time } = wallClock(ended.toISOString());
		setDateField(form.elements.date, date);
		form.elements.time.value = time;
	})()`, nil))

	p.run("book it", p.click(`#form-timer-end button[type="submit"]`),
		chromedp.WaitNotVisible("#timer-banner", chromedp.ByID))

	var hours float64

	p.awaitJSON(`api('/timesheets').then((list) => list.items[0]?.durationHours ?? 0)`, &hours)

	// Eight hours, less the seconds a time field cannot say.
	if hours < 7.97 || hours > 8.001 {
		t.Errorf("the booking holds %v h, want eight", hours)
	}
}

// waitForTextPrefix waits until an element's text begins with the given words.
func (p *page) waitForTextPrefix(selector, prefix string) {
	p.t.Helper()

	deadline := time.Now().Add(waitPatience)

	for time.Now().Before(deadline) {
		if strings.HasPrefix(p.text(selector), prefix) {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	p.t.Fatalf("%s never began with %q; it reads %q", selector, prefix, p.text(selector))
}

// awaitJSON evaluates an expression that may be a promise, and unpacks its value.
//
// evalJSON's sibling for the cases that set up state through the page's own api():
// that returns a promise, and an evaluation that does not wait for it hands back
// the pending promise rather than what the server said.
func (p *page) awaitJSON(expression string, into any) {
	p.t.Helper()

	var raw string

	p.run("wait for the page's answer", chromedp.Evaluate(
		`(async () => JSON.stringify(await (`+expression+`)) ?? 'null')()`, &raw, awaitPromise))

	if into == nil {
		return
	}

	if err := json.Unmarshal([]byte(raw), into); err != nil {
		p.t.Fatalf("reading the page's answer: %v\n\n%.400s", err, raw)
	}
}
