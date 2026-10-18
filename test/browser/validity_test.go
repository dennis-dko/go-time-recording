//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A field the browser refuses is refused in the screen's language.
//
// The forms state their rules in the markup - a required field, hours between
// 0.01 and 24, a description of at most 2,000 characters - and the browser holds
// a form to them before anything is sent, in its own words: on a German screen in
// an English browser, "Value must be less than or equal to 24." and "Please fill
// out this field." The words are the application's now, in the screen's
// language; decided on 2026-10-17.
//
// A refusal of its own makes a field invalid until it is taken back, so a value
// the script puts in afterwards - which raises no input event - must not meet the
// old refusal.
func TestABrowserRefusalSpeaksTheScreensLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()
	p.chooseLanguage("de")

	p.run("open the time entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID))

	const hours = `#form-timesheet input[name="durationHours"]`

	for _, attempt := range []struct{ value, want string }{
		{"30", "Höchstens 24."},
		{"0.001", "Mindestens 0,01."},
		{"", "Bitte fülle dieses Feld aus."},
	} {
		var said string

		p.run("book "+attempt.value,
			chromedp.SetValue(hours, attempt.value, chromedp.ByQuery),
			p.click(`#form-timesheet button[type="submit"]`),
			chromedp.Evaluate(`document.querySelector('`+hours+`').validationMessage`, &said))

		if said != attempt.want {
			t.Errorf("booking %q is refused with %q, where the German screen would say %q",
				attempt.value, said, attempt.want)
		}
	}

	// Assigned the way the script fills a form, which raises no event at all -
	// chromedp's SetValue raises input and change, and would take the old refusal
	// back by the ordinary road.
	p.run("put a good figure in from the script and book it",
		chromedp.Evaluate(`document.querySelector('`+hours+`').value = '8'`, nil),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "8")
}
