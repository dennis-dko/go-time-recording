//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Nothing on the settings screen sends somebody to restart the application for
// a log level.
//
// A saved level applies from the next line: the logging card says so, and the
// integration suite's TestTheLogLevelAppliesWithoutARestart holds it. The log
// card beneath it went on saying the level takes effect at the next start, and
// said it twice - in its hint, and in the sentence that appears when a level is
// ticked that the process is not writing, which is exactly when somebody is
// about to change it. The tour said the same of all three settings on the
// logging card. An administrator who believed the nearer sentence restarted an
// application they were in the middle of diagnosing.
//
// In both languages, because the German sentence also named the logging card
// by a title no card on the screen has.
func TestNothingSaysTheLogLevelWaitsForARestart(t *testing.T) {
	t.Parallel()

	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line")

	// DEBUG is below what this process writes, so ticking it is what brings the
	// sentence up.
	p.run("tick a level the process is not writing",
		p.click(`#log-levels input[value="DEBUG"]`))

	p.waitShown("#log-level-warning")

	for _, language := range []struct{ code, waits, atOnce string }{
		{"en", "next start", "at once"},
		{"de", "nächsten Start", "sofort"},
	} {
		p.chooseLanguage(language.code)

		card := p.text(`h2[data-i18n="tel.title"]`)

		var tour string

		p.run("read what the tour says about the logging card", chromedp.Evaluate(
			`TOUR_STEPS.find((step) => step.target === '#form-telemetry').text()`, &tour))

		// The sentence the logging card itself opens with is the one that is
		// right, and the tour describes that card.
		if !strings.Contains(p.text(`[data-i18n="tel.hint"]`), language.atOnce) {
			t.Fatalf("%s: the logging card no longer says the level applies %s: %q",
				language.code, language.atOnce, p.text(`[data-i18n="tel.hint"]`))
		}

		if !strings.Contains(tour, language.atOnce) {
			t.Errorf("%s: the tour does not say the level applies %s: %q", language.code, language.atOnce, tour)
		}

		for place, said := range map[string]string{
			"the log card's hint":                  p.text(`[data-i18n="log.hint"]`),
			"the sentence under the ticked levels": p.text("#log-level-warning"),
		} {
			if strings.Contains(said, language.waits) {
				t.Errorf("%s: %s sends the reader to the %s for a level that applies %s: %q",
					language.code, place, language.waits, language.atOnce, said)
			}

			if !strings.Contains(said, card) {
				t.Errorf("%s: %s does not name the card the level is set on, %q: %q",
					language.code, place, card, said)
			}
		}
	}
}
