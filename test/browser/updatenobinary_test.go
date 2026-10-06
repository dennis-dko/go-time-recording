//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The update card says why a newer version is not offered when the release has
// nothing for this platform, rather than describing a download beside no button.
func TestTheUpdateCardSaysWhenAReleaseHasNothingForThisPlatform(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.settleReleaseWatch()
	p.chooseLanguage("de")

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-check", chromedp.ByID))

	// What the server answers for a newer release without a build for this
	// platform - measured against the handler in the rest package's own case.
	p.run("answer with a release that has nothing for this platform", chromedp.Evaluate(`
		(() => {
			const server = api;
			api = (path, options) => path === '/settings/update/check'
				? Promise.resolve({ running: 'v1.0.0', latest: 'v9.9.9', newer: true,
					available: false, installable: true, why: 'noBinary', enabled: true,
					comparable: true, restartable: true, url: 'https://example.com/r' })
				: server(path, options);
			return 1;
		})()`, nil))

	p.run("press it", p.click("#update-check"))
	p.waitForText("#update-state", "v9.9.9")

	hint := p.text("#update-hint")

	if p.visible("#update-now") {
		t.Fatal("the card offers to install a release that has nothing for this platform")
	}

	// "Der Download wird gegen die Prüfsumme des Releases geprüft ..." is what the
	// card says of the download the button starts.
	if strings.Contains(hint, "Prüfsumme") {
		t.Errorf("beside no button, the card describes the download it would check: %q", hint)
	}

	if !strings.Contains(hint, "Plattform") {
		t.Errorf("the card does not say the release has nothing for this platform: %q", hint)
	}
}
