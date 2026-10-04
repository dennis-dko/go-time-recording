//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// A line in the log viewer says when it was written: on which day, and in the
// zone the rest of the screen uses.
//
// The viewer wrote the time of day and nothing else, in the browser's zone. Its
// buffer holds the last five thousand lines, which on a quiet installation - or
// one logging at WARN, as the manual asks of one with a schedule - reach back
// days, so "03:00:01 ERROR" could not be told from this morning. And every other
// moment on the screen is written in the zone the account chose, so on a device
// set to another zone the log and the token list beside it disagreed about the
// same minute.
func TestALogLineSaysOnWhichDayAndInWhichZoneItWasWritten(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.chooseLanguage("de")

	var zone int

	p.run("live in Tokyo, on a device set to UTC",
		emulation.SetTimezoneOverride("UTC"),
		chromedp.Evaluate(`(async () => {
			const token = document.cookie.split('; ')
				.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
			const res = await fetch('/api/v1/me/timezone', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
				body: JSON.stringify({ timezone: 'Asia/Tokyo' }),
			});
			return res.status;
		})()`, &zone, awaitPromise))

	if zone != 200 {
		t.Fatalf("choosing a zone for the account answered %d", zone)
	}

	// Reloaded, so the page reads the account's zone the way it does at start.
	p.run("pick the zone up",
		chromedp.Reload(),
		chromedp.WaitVisible(`html[data-loaded="yes"]`, chromedp.ByQuery))

	var shown []string

	p.run("show two lines", chromedp.Evaluate(`(() => {
		appendLogLines([
			{ seq: 1, time: '2026-10-01T03:00:01Z', level: 'ERROR', message: 'days ago' },
			{ seq: 2, time: new Date().toISOString(), level: 'INFO', message: 'just now' },
		]);
		return [...document.querySelectorAll('#log-output .log-time')]
			.slice(-2).map((node) => node.textContent);
	})()`, &shown))

	if len(shown) != 2 {
		t.Fatalf("the viewer shows %d times for the two lines: %q", len(shown), shown)
	}

	// 03:00:01 in Greenwich is noon in Tokyo, on the first of October.
	if !strings.Contains(shown[0], "01.10.2026") {
		t.Errorf("a line from three days ago is shown as %q, without the day it was written",
			shown[0])
	}

	if !strings.Contains(shown[0], "12:00:01") {
		t.Errorf("a line written at 03:00:01 UTC is shown as %q, which is not the time in "+
			"the account's zone, Tokyo", shown[0])
	}

	if strings.Contains(shown[1], "202") {
		t.Errorf("a line written a moment ago carries a date it does not need: %q", shown[1])
	}
}
