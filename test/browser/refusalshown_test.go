//go:build browser

package browser

import (
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
	p.run("book over the ceiling",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "9", chromedp.ByQuery),
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
