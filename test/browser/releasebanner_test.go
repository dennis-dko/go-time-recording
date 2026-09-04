//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The release banner fills in both versions wherever a translation puts them.
//
// The sentence is split at {0} so the new version can be the control rather than
// a button beside it, and the comment above that split states the freedom it is
// buying: "where the number falls in the sentence is the translator's business,
// and the two halves are whatever they wrote around it."
//
// That holds for {0} and did not hold for {1}. Only the half after the split was
// run through fillIn, so a sentence that names the running version before the new
// one - which is ordinary word order in plenty of languages - put a literal {1}
// on the banner across the top of every screen.
//
// German happens to put them the other way round, so nothing showed it, and
// nothing would: the placeholder check compares which values a translation
// carries, not where it puts them.
func TestTheReleaseBannerFillsInBothVersionsInAnyOrder(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// A translation that names the running version first. Written into the table
	// rather than into the file, because the fault is in the reading of it and any
	// sentence with the two in this order shows it.
	p.run("a sentence the other way round", chromedp.Evaluate(
		`TRANSLATIONS.de['update.available'] =
			'Diese Installation läuft mit {1}. Version {0} ist verfügbar.'`, nil))

	p.chooseLanguage("de")

	p.run("a newer version exists", chromedp.Evaluate(
		`showReleaseState({ newer: true, latest: 'v9.9.9', running: 'v0.0.1' })`, nil))

	p.waitShown("#release-banner")

	var said string

	p.run("read the banner", chromedp.Evaluate(
		`document.querySelector('#release-text').textContent.trim()`, &said))

	if strings.Contains(said, "{") {
		t.Errorf("the banner reads %q, with a placeholder showing. Only the half "+
			"after the split is filled in, so a translation that names the running "+
			"version before the new one leaves its brace on screen", said)
	}

	for _, version := range []string{"v9.9.9", "v0.0.1"} {
		if strings.Contains(said, version) {
			continue
		}

		t.Errorf("the banner reads %q and does not name %s", said, version)
	}
}
