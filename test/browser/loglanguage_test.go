//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The sentence under the log's level boxes changes language with the screen.
//
// It was drawn when the logging card loaded and when a box was ticked, and by
// nothing else, so after a change of language it stood in the language switched
// from until the card's next answer arrived - a request later. A case reading it
// in that moment saw English on a German screen, on one CI run and not on the
// others. The card's answer is held back here, so the moment is the one tested
// rather than one the machine may or may not pick.
func TestTheLogLevelWarningChangesLanguageWithTheScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("ask for a level the process is not writing", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID),
		p.click(`#log-levels input[value="DEBUG"]`))

	p.waitShown("#log-level-warning")

	if said := p.text("#log-level-warning"); !strings.Contains(said, "stay empty") {
		t.Fatalf("the warning is not the English sentence this case starts from: %q", said)
	}

	p.run("hold back the logging card's next answer", chromedp.Evaluate(`(() => {
		const server = api;
		api = (path, options) => String(path).startsWith('/settings/telemetry')
			? new Promise(() => {})
			: server(path, options);
		return 1;
	})()`, nil))

	p.chooseLanguage("de")

	said := p.text("#log-level-warning")

	if strings.Contains(said, "stay empty") || !strings.Contains(said, "leer") {
		t.Errorf("after switching to German the warning reads %q", said)
	}
}
