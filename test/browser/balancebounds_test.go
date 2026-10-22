//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A running balance past a bound its owner set raises a banner on every screen,
// whose link leads back to the bounds, and the banner goes once the balance is
// inside again - here by clearing the bound. The sentence is read in English and
// in German, with the figures written the way the overtime screen writes them.
func TestABalancePastItsBoundRaisesABannerUntilItIsInsideAgain(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var booked bool

	// A Friday long past, an hour and a half over the default eight.
	p.awaitJSON(`(async () => {
		await api('/timesheets', {
			method: 'POST', body: JSON.stringify({ date: '2026-07-03', durationHours: 9.5 }),
		});
		await loadNotices();
		return true;
	})()`, &booked)

	if p.visible("#balance-banner") {
		t.Fatal("the banner stands before any bound was set")
	}

	p.showView("settings")
	p.waitForText("#balance-now", "+1.50 h")

	p.run("set an upper bound of one hour",
		chromedp.SetValue(`#form-balance-bounds input[name="upperBound"]`, "1", chromedp.ByQuery),
		p.click(`#form-balance-bounds button[type="submit"]`))

	p.waitShown("#balance-banner")

	const english = "Your running balance is +1.50 h, above the upper bound of +1.00 h you set under My account."
	if said := strings.TrimSpace(p.text("#balance-banner")); said != english {
		t.Errorf("the banner says %q, want %q", said, english)
	}

	p.showView("timesheets")
	p.run("follow the banner", p.click("#balance-banner-open"),
		chromedp.WaitVisible("#form-balance-bounds", chromedp.ByID))

	if view := p.currentView(); view != "settings" {
		t.Errorf("the banner's link opened %q, want My account", view)
	}

	p.chooseLanguage("de")
	p.waitForText("#balance-banner", "Obergrenze")

	const german = "Dein laufender Saldo liegt mit +1,50 Std. über der Obergrenze von +1,00 Std., " +
		"die du unter Mein Konto gesetzt hast."
	if said := strings.TrimSpace(p.text("#balance-banner")); said != german {
		t.Errorf("the banner says %q in German, want %q", said, german)
	}

	p.run("clear the bound",
		chromedp.SetValue(`#form-balance-bounds input[name="upperBound"]`, "", chromedp.ByQuery),
		p.click(`#form-balance-bounds button[type="submit"]`))

	p.waitGone("#balance-banner")
}
