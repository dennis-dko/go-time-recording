//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// chooseLanguage returns once every screen has been drawn again, not once the
// language is in force.
//
// The two are a reload apart, and a case reading a card the script writes - here
// the sentence under the log's level boxes - between them read the language just
// left. CI fell into that window in TestNothingSaysTheLogLevelWaitsForARestart
// with the logging card's answer a few milliseconds late. It is held back for a
// second and a half here, before the request is counted, so the window is wide
// open and a helper that waits only for the language reads English every time.
func TestChoosingALanguageWaitsForTheScreensToBeDrawnAgain(t *testing.T) {
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
		api = async (path, options) => {
			if (String(path).startsWith('/settings/telemetry')) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
			}

			return server(path, options);
		};
		return 1;
	})()`, nil))

	p.chooseLanguage("de")

	said := p.text("#log-level-warning")

	if strings.Contains(said, "stay empty") || !strings.Contains(said, "leer") {
		t.Errorf("chooseLanguage returned with the warning reading %q", said)
	}
}
