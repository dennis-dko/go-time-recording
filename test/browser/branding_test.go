//go:build browser

package browser

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A configured text may carry a date that stays current and a link that works.
//
// The banner, the footer and the legal notice are written once and read for
// years, and what dates them is exactly what nobody comes back to change: a
// copyright year, a version, the instance's own name after a rename. So those are
// written as placeholders and worked out when the page is drawn.
//
// Not HTML, and not a rich text editor. These three are shown on the sign-in
// screen, before anybody has authenticated - so whatever is written here is
// rendered for every visitor, including the next administrator. Accepting markup
// would mean accepting a script tag from anyone holding settings:manage.
func TestAConfiguredTextFillsInPlaceholdersAndMakesLinks(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, "")

	p.setBrandingText(t, "footerText",
		"© {year} Beispiel GmbH — [Impressum](https://example.com/impressum)")

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	footer := p.text("#footer-text")

	year := strconv.Itoa(time.Now().Year())
	if !strings.Contains(footer, year) {
		t.Errorf("the footer reads %q; {year} was not filled in", footer)
	}

	if strings.Contains(footer, "{year}") {
		t.Errorf("the placeholder is shown as written: %q", footer)
	}

	// The link is a link, and it goes where it says.
	var href string

	p.run("read the link", chromedp.Evaluate(
		`document.querySelector('#footer-text a')?.href ?? ''`, &href))

	if href != "https://example.com/impressum" {
		t.Errorf("the footer's link points at %q", href)
	}

	// And the words around it are still words rather than markup.
	if strings.Contains(footer, "[Impressum]") {
		t.Errorf("the link is shown as its source: %q", footer)
	}
}

// Nothing written into a configured text can be made to run.
//
// The reason this is a grammar rather than an editor. Everything here is built as
// DOM nodes and never assigned as innerHTML, so a tag is a tag-shaped string; and
// the only schemes made into links are the three that go somewhere.
func TestAConfiguredTextCannotSmuggleSomethingIn(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.setBrandingText(t, "footerText",
		`<img src=x onerror="window.__ran=1"> [go](javascript:window.__ran=1)`)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	var ran bool

	p.run("did anything run", chromedp.Evaluate(`Boolean(window.__ran)`, &ran))

	if ran {
		t.Fatal("a configured text ran something")
	}

	// The tag is shown as the words somebody typed rather than becoming an
	// element.
	if p.count("#footer-text img") != 0 {
		t.Error("a configured text became markup")
	}

	if !strings.Contains(p.text("#footer-text"), "<img") {
		t.Errorf("the tag was silently dropped instead of shown: %q",
			p.text("#footer-text"))
	}

	// And a javascript: address is not made into a link at all.
	if p.count("#footer-text a") != 0 {
		t.Error("a javascript: address was made into a link")
	}
}

// setBrandingText stores one branding field through the API.
func (p *page) setBrandingText(t *testing.T, field, value string) {
	t.Helper()

	var status string

	p.run("store "+field, chromedp.Evaluate(fmt.Sprintf(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Zeiterfassung', %s: %q }),
			});

			return String(r.status);
		})()`, field, value), &status, awaitPromise))

	if status != "200" {
		t.Fatalf("could not store %s: HTTP %s", field, status)
	}
}
