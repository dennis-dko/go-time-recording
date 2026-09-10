//go:build browser

package browser

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Signing out takes the appearance with it.
//
// Appearance is chosen per device, which is right while somebody is using it
// and wrong the moment they leave: the next person at that machine arrived to
// the last one's dark mode, on a screen with nothing else of theirs on it.
func TestSigningOutForgetsTheAppearance(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("choose dark", p.chooseOption("#theme-picker", "dark"))

	var chosen struct {
		Preference string `json:"preference"`
		Stored     string `json:"stored"`
	}

	p.evalJSON(`JSON.stringify({
		preference: document.documentElement.dataset.themePreference,
		stored: localStorage.getItem('gtr.theme') ?? '',
	})`, &chosen)

	if chosen.Preference != "dark" || chosen.Stored != "dark" {
		t.Fatalf("choosing dark left the page at %+v, so this proves nothing", chosen)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var after struct {
		Preference string `json:"preference"`
		Stored     string `json:"stored"`
		Picker     string `json:"picker"`
	}

	p.evalJSON(`JSON.stringify({
		preference: document.documentElement.dataset.themePreference,
		stored: localStorage.getItem('gtr.theme') ?? '',
		picker: document.querySelector('#theme-picker').value,
	})`, &after)

	if after.Stored != "" {
		t.Errorf("the appearance %q is still kept on this device after signing out",
			after.Stored)
	}

	if after.Preference != "auto" {
		t.Errorf("the page follows %q rather than the time of day after signing out",
			after.Preference)
	}

	if after.Picker != "auto" {
		t.Errorf("the picker still offers %q as the current choice", after.Picker)
	}
}

// The picker writes to localStorage and stamps the document; both have to
// happen or the choice is lost on the next page load.
func TestTheAppearancePickerChangesTheTheme(t *testing.T) {
	t.Parallel()

	p := open(t)

	for _, want := range []string{"dark", "light"} {
		p.run("choose "+want, chromedp.SetValue("#theme-picker", want, chromedp.ByID),
			chromedp.Evaluate(
				`document.querySelector('#theme-picker').dispatchEvent(new Event('change'))`, nil))

		p.waitEvaluates("the theme", `document.documentElement.dataset.theme`, want)
	}
}
