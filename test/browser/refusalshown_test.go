//go:build browser

package browser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A refusal from the server reaches the reader in their own language.
//
// The messages are written where the rule is enforced, in English, which is right
// for the log and wrong for the person who tripped over it: an English sentence was
// shown to a German reader whatever they had chosen.
// The reason now travels as a code with the values the sentence interpolated, and
// the interface looks the sentence up.
//
// Proved in a browser because that is the only place the lookup happens - on the
// wire the message is still English, deliberately, and the integration tests check
// exactly that.
func TestAServerRefusalIsShownInTheReadersLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Two accounts in order, because the two halves belong to different jobs. The
	// instance-wide ceiling is configuration and only the built-in administrator may
	// set it; the booking that runs into it is a working day, which that account does
	// not have. Under My account there is a per-account ceiling, and that is not the
	// one this case is about.
	p.run("set a daily ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.SetValue(`#form-operational input[name="maxDailyHours"]`, "8", chromedp.ByQuery),
		p.click(`#form-operational button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.becomeWorker()

	// German after the change of account, not before: the language is a preference of
	// whoever is signed in, so choosing it as the administrator would have set it for
	// the wrong person and left this reader in English.
	p.chooseLanguage("de")

	p.run("clear the notices", chromedp.Evaluate(
		`document.querySelector('#toast').replaceChildren()`, nil))

	// A booking over the ceiling: a refusal with four values in it, which is the case
	// that would fall apart if the values were dropped.
	var day string

	p.run("book over the ceiling",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "9", chromedp.ByQuery),
		// The day the form holds, as it travels: the named box is the native one.
		chromedp.Value(`#form-timesheet input[name="date"]`, &day, chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); shown != "" {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if shown == "" {
		t.Fatalf("the refusal raised no notice at all\n\napplication log:\n%s", p.app.Log())
	}

	// The German sentence, not the English one it was built from.
	if !strings.Contains(shown, "Tagesmaximum") {
		t.Errorf("the notice is not the German sentence: %q", shown)
	}

	if strings.Contains(shown, "daily limit") {
		t.Errorf("the notice is still the server's English wording: %q", shown)
	}

	// And the figures survived the translation - 9 booked against a ceiling of 8.
	for _, figure := range []string{"9", "8"} {
		if !strings.Contains(shown, figure) {
			t.Errorf("the notice lost the figure %s: %q", figure, shown)
		}
	}

	// And the day is written the way every other day on this screen is: in the
	// reader's order, not in the order it travels in. The sentence was German and
	// the day in it was "2026-10-04".
	when, err := time.Parse(time.DateOnly, day)
	if err != nil {
		t.Fatalf("the form held %q as its day, which is not one: %v", day, err)
	}

	if german := when.Format("02.01.2006"); !strings.Contains(shown, german) {
		t.Errorf("the notice does not write the day as %s, the way the form beside it "+
			"does: %q", german, shown)
	}

	if strings.Contains(shown, day) {
		t.Errorf("the notice writes the day as it travels, %s: %q", day, shown)
	}
}

// A status in a refusal is called what the screen calls it.
//
// A project that no longer takes bookings is refused by its status, and the
// sentence put the status in as the server stores it: "Projekt „Brücke“ ist
// completed", on a screen whose status badge, filter and form all say
// "abgeschlossen". A reader looking for the word the refusal used would find it
// nowhere.
//
// Reached the ordinary way: the project is chosen in the booking form and
// completed from somewhere else before the form is sent - a second tab, or the
// owner finishing it on another screen.
func TestARefusalNamesAStatusTheWayTheScreenDoes(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.chooseLanguage("de")

	const named = "Brücke"

	var id float64

	p.run("make a project", chromedp.Evaluate(`(async () => {
		const token = document.cookie.split('; ')
			.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
		const res = await fetch('/api/v1/projects', {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
			body: JSON.stringify({ name: '`+named+`', startDate: '2026-01-05' }),
		});
		return res.ok ? (await res.json()).data.id : -res.status;
	})()`, &id, awaitPromise))

	if id <= 0 {
		t.Fatalf("the project could not be made: %v", id)
	}

	// Reloaded, because the lists were filled before the project existed.
	p.run("choose it in the form",
		chromedp.Reload(),
		chromedp.WaitVisible(`html[data-loaded="yes"]`, chromedp.ByQuery),
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet select[name="projectId"]`, strconv.Itoa(int(id)),
			chromedp.ByQuery),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "1", chromedp.ByQuery),
	)

	var completed int

	p.run("complete it elsewhere", chromedp.Evaluate(`(async () => {
		const token = document.cookie.split('; ')
			.find((c) => c.startsWith('gtr_csrf=')).split('=')[1];
		const res = await fetch('/api/v1/projects/`+strconv.Itoa(int(id))+`', {
			method: 'PUT',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
			body: JSON.stringify({ status: 'completed' }),
		});
		return res.status;
	})()`, &completed, awaitPromise))

	if completed != 200 {
		t.Fatalf("completing the project answered %d", completed)
	}

	p.run("clear the notices", chromedp.Evaluate(
		`document.querySelector('#toast').replaceChildren()`, nil))

	p.run("book on it", p.click(`#form-timesheet button[type="submit"]`))

	shown := ""

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); strings.Contains(shown, named) {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !strings.Contains(shown, named) {
		t.Fatalf("booking on a completed project raised no notice naming it: %q\n\n"+
			"application log:\n%s", shown, p.app.Log())
	}

	if !strings.Contains(shown, "abgeschlossen") {
		t.Errorf("the notice does not call the status what the screen calls it: %q", shown)
	}

	if strings.Contains(shown, "completed") {
		t.Errorf("the notice names the status the way it is stored: %q", shown)
	}
}

// A field the server rejects is named the way the form names it.
func TestARejectedFieldIsNamedNotIdentified(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.chooseLanguage("de")

	// A negative ceiling, which is refused by field rather than by sentence.
	p.run("save an impossible ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const field = document.querySelector('#form-operational input[name="maxDailyHours"]');
			field.value = '-3';
			field.removeAttribute('min');

			// Announced the way typing announces itself, which is what tells the
			// page somebody is part way through this form. Without it a loader
			// answering between here and the press below puts the stored ceiling
			// back, the save is a valid one, and the case waits for a refusal
			// that was never going to come - which is what it reported, as a
			// refusal that named no field.
			field.dispatchEvent(new Event('input', { bubbles: true }));
		})()`, nil),
		p.click(`#form-operational button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); strings.Contains(shown, "Max/Tag") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !strings.Contains(shown, "Max/Tag") {
		t.Errorf("the refusal does not name the field as the form does: %q\n\n"+
			"application log:\n%s", shown, p.app.Log())
	}

	if strings.Contains(shown, "maxDailyHours") {
		t.Errorf("the refusal still shows the column name: %q", shown)
	}

	if strings.Contains(shown, "invalid parameter") {
		t.Errorf("the refusal still carries GoFr's parameter count: %q", shown)
	}
}
