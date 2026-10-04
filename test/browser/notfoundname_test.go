//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A refusal for something that is not there names it the way the screen does.
//
// A correction or a deletion of an entry that is gone by the time it lands - a
// second tab, a colleague's import that replaced it - is answered "not found",
// with what was looked for as a value. The value is the server's own word for
// it, so a German screen read "timesheet mit der Kennung 42 wurde nicht
// gefunden."
func TestANotFoundRefusalNamesWhatWasLookedForInTheReadersWords(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.chooseLanguage("de")

	var said string

	p.run("correct an entry that is not there", chromedp.Evaluate(`(async () => {
		const token = document.cookie.split('; ')
			.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
		const res = await fetch('/api/v1/timesheets/999999', {
			method: 'PUT',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
			body: JSON.stringify({ durationHours: 1 }),
		});
		const body = await res.json();
		return res.status + ' ' + describeRefusal(body.error);
	})()`, &said, awaitPromise))

	if !strings.HasPrefix(said, "404 ") {
		t.Fatalf("correcting an entry that is not there answered %q, want a 404", said)
	}

	if strings.Contains(said, "timesheet") {
		t.Errorf("the refusal names the entry by the server's word for it: %q", said)
	}

	if !strings.Contains(said, "Zeiteintrag") {
		t.Errorf("the refusal does not call the entry what the screen calls it: %q", said)
	}
}
