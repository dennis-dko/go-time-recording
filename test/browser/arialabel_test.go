//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// A label read only by assistive technology comes back to English with the rest
// of the page.
//
// applyLanguage translates four kinds of thing, and three of them take a copy of
// the English source the first time they are translated and put it back when the
// dictionary has nothing: the element text, the placeholder, the title. The
// fourth, aria-label, does neither - it assigns when there is a translation and
// does nothing at all when there is not.
//
// TRANSLATIONS.en is deliberately empty, so that "nothing" is exactly what
// English is. Going to German and back therefore left every translated
// aria-label in German on an otherwise English page. Six elements carry one,
// among them the navigation menu, the appearance picker and the language picker
// itself - so the control somebody uses to get back to English is the one that
// stays German, and only for the readers who cannot see the label beside it.
//
// Checked on the language picker because its label is the one this case can be
// sure of, and the failure is the same for all six.
func TestATranslatedAriaLabelComesBackToEnglish(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	read := func(what string) string {
		t.Helper()

		var out string

		p.run("read the label "+what, chromedp.Evaluate(
			`document.querySelector('#language-picker').getAttribute('aria-label') ?? ''`,
			&out))

		return out
	}

	english := read("to begin with")

	if english == "" {
		t.Fatal("the language picker has no aria-label at all, so this case is not " +
			"measuring what it thinks it is")
	}

	p.chooseLanguage("de")

	german := read("in German")

	if german == english {
		t.Fatalf("the label did not change with the language (still %q), so going "+
			"back cannot be tested", english)
	}

	p.chooseLanguage("en")

	back := read("back in English")

	if back != english {
		t.Errorf("after German and back the label reads %q rather than %q. "+
			"applyLanguage keeps a copy of the English source for the text, the "+
			"placeholder and the title and puts it back when the dictionary is "+
			"empty; the aria-label is the one that does not, and English is exactly "+
			"the empty dictionary", back, english)
	}
}

// And the same for every element that carries one, so the next translated label
// is covered rather than only the one this was found on.
func TestNoTranslatedAriaLabelIsLeftInTheOtherLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	labels := func(when string) map[string]string {
		t.Helper()

		var out string

		p.run("read every label "+when, chromedp.Evaluate(`JSON.stringify(
			Object.fromEntries([...document.querySelectorAll('[data-i18n-aria]')]
				.map((n) => [n.dataset.i18nAria, n.getAttribute('aria-label') ?? ''])))`,
			&out))

		var read map[string]string

		if err := json.Unmarshal([]byte(out), &read); err != nil {
			t.Fatalf("reading the labels (%q): %v", out, err)
		}

		return read
	}

	before := labels("to begin with")

	if len(before) == 0 {
		t.Fatal("no element carries a translated aria-label, so this case checks nothing")
	}

	p.chooseLanguage("de")
	p.chooseLanguage("en")

	after := labels("after German and back")

	for key, was := range before {
		if after[key] == was {
			continue
		}

		t.Errorf("%s reads %q after German and back, where it began as %q",
			key, after[key], was)
	}
}
