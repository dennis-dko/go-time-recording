//go:build browser

package browser

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
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

// An instance that has configured nothing still has a mark.
//
// The header and the sign-in screen used to be empty until somebody uploaded
// something, which is a header of words alone and a sign-in card with a gap where
// a mark goes - on every installation on its first day. The tab has had a default
// all along; these two now have the same one.
func TestTheShippedMarkStandsInUntilALogoIsConfigured(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Nothing configured: this instance has never been given a logo.
	//
	// Asked of the source rather than of visibility, because the sign-in screen is
	// hidden while somebody is signed in - its mark is filled in and waiting, which
	// is exactly what has to be true for the next visitor to see it.
	for _, holder := range []string{"#brand-logo", "#login-logo"} {
		if src := p.attr(holder, "src"); !strings.Contains(src, "favicon.svg") {
			t.Errorf("%s shows %.40q rather than the shipped mark", holder, src)
		}
	}

	// And the header's is on the screen, which is the one of the two that is.
	if !p.visible("#brand-logo") {
		t.Error("the header has no mark at all on an instance with no logo")
	}

	// And a configured one replaces it rather than sitting beside it.
	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	if src := p.attr("#brand-logo", "src"); !strings.HasPrefix(src, "data:image/") {
		t.Errorf("the header still shows %.40q after a logo was configured", src)
	}
}

// Your own row says so, and says it before it says anything else.
//
// The built-in administrator's row already said "system account", and the row
// somebody reads their own name in said nothing at all - which is the one fact
// that matters to the person looking at the table, because it is the row whose
// delete button is not there.
func TestYourOwnRowIsMarkedAsYours(t *testing.T) {
	p := open(t)
	p.readyAdmin()
	p.createOrdinaryAccount(t, "sven@example.com", "sven-password-1")

	// Reloaded, because the account above was created through the API rather than
	// through the form: the table is filled when the interface loads and refreshed
	// by what the form does afterwards.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.settleWelcome()

	p.run("open the accounts", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.waitForText("#table-users tbody", "sven@example.com")

	var own struct {
		Label     string `json:"label"`
		HasDelete bool   `json:"hasDelete"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const rows = [...document.querySelectorAll('#table-users tbody tr')];
		const mine = rows.find(row => row.textContent.includes('admin@local'));
		if (!mine) return { label: 'no row for the signed-in account', hasDelete: false };

		return {
			label: mine.querySelector('td.actions span.muted')?.textContent ?? '',
			hasDelete: Boolean(mine.querySelector('td.actions button.danger')),
		};
	})())`, &own)

	if own.Label != "Your account" {
		t.Errorf("the signed-in account's row is marked %q, want \"Your account\"",
			own.Label)
	}

	if own.HasDelete {
		t.Error("the row for the account doing the looking offers a delete, which " +
			"would end the session it was pressed from")
	}
}

// The sign-in screen can be used on a short window.
//
// It is a fixed overlay that centres what it holds, which clips anything too
// tall at both ends - and nothing scrolls it back. The submit button is simply
// not there, on a screen where nothing looks wrong: the form is visible, one
// button short of the bottom.
//
// A laptop with a toolbar is enough to do it once the screen has a notice above
// the card and a mark above that. Which is how this was found: the shipped mark
// started standing in for an absent logo, and a sign-in on a short CI window
// stopped working.
//
// No sign-in needed - open(t) lands here, which is the state this is about.
func TestTheSignInScreenIsUsableOnAShortWindow(t *testing.T) {
	p := open(t)

	// Shorter than the screen's own contents, so this asks about the property
	// rather than about one machine's idea of a window.
	p.run("a short window", chromedp.EmulateViewport(1000, 320),
		chromedp.Sleep(400*time.Millisecond))

	var out struct {
		Overflows    bool    `json:"overflows"`
		Scrolled     float64 `json:"scrolled"`
		ButtonBottom float64 `json:"buttonBottom"`
		Viewport     float64 `json:"viewport"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const screen = document.querySelector('#login-screen');
		const button = document.querySelector('#form-login button[type="submit"]');

		// What a person does when a form runs off the edge, and what they cannot
		// do if the overlay does not scroll.
		screen.scrollTop = screen.scrollHeight;

		return {
			overflows: screen.scrollHeight > screen.clientHeight + 1,
			scrolled: screen.scrollTop,
			buttonBottom: button.getBoundingClientRect().bottom,
			viewport: window.innerHeight,
		};
	})())`, &out)

	if !out.Overflows {
		t.Skip("the sign-in screen fits in 320px, so there is nothing to scroll here")
	}

	if out.Scrolled == 0 {
		t.Fatal("the sign-in screen is taller than the window and will not scroll, " +
			"so whatever is past the edge cannot be reached at all")
	}

	if out.ButtonBottom > out.Viewport {
		t.Errorf("the submit button ends at %.0fpx in a %.0fpx window even after "+
			"scrolling to the bottom", out.ButtonBottom, out.Viewport)
	}
}

// The texts an installation writes about itself can be written twice.
//
// A title, a banner, a footer and a legal notice are the only words on the screen
// this application does not supply, so they were the only ones a language switch
// could not reach: a German reader got the English banner because there was only
// one banner.
//
// One language at a time on the form, because eight fields at once is a grid to
// squint at - and an installation working in one language never opens the
// switcher at all.
func TestTheConfiguredTextsFollowTheReadersLanguage(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// English first, which is the base: what a reader gets when their own
	// language has nothing written for it.
	p.run("write the English texts",
		p.chooseOption("#branding-language", "en"),
		chromedp.SetValue(`#form-branding input[name="banner"]`, "Company outing on Friday",
			chromedp.ByQuery),
		chromedp.SetValue(`#form-branding input[name="footerText"]`, "Made in Osnabrück",
			chromedp.ByQuery))

	p.run("and the German ones",
		p.chooseOption("#branding-language", "de"),
		chromedp.SetValue(`#form-branding input[name="banner"]`, "Betriebsausflug am Freitag",
			chromedp.ByQuery),
		chromedp.SetValue(`#form-branding input[name="footerText"]`, "Gemacht in Osnabrück",
			chromedp.ByQuery),
		p.click(`#form-branding button[type="submit"]`))

	p.waitForText("#instance-banner", "outing")

	// The reader is on English, so that is what they get.
	if got := p.text("#instance-banner"); !strings.Contains(got, "Company outing") {
		t.Errorf("an English reader is shown %q", got)
	}

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	// And the banner follows, without asking the server again or reloading.
	p.waitForText("#instance-banner", "Betriebsausflug")

	if got := p.text("#footer-text"); !strings.Contains(got, "Gemacht") {
		t.Errorf("the footer still reads %q after switching to German", got)
	}
}

// A language nobody has written for falls back to the base rather than to
// nothing.
//
// The case that decides whether this feature is safe to have at all: an
// installation that fills in one language and never opens the switcher must go on
// working exactly as it did, for every reader.
func TestALanguageWithNoTextsFallsBackRatherThanEmptying(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("write one language only",
		p.chooseOption("#branding-language", "en"),
		chromedp.SetValue(`#form-branding input[name="banner"]`, "One banner for everybody",
			chromedp.ByQuery),
		p.click(`#form-branding button[type="submit"]`))

	p.waitForText("#instance-banner", "One banner")

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	if got := p.text("#instance-banner"); !strings.Contains(got, "One banner") {
		t.Errorf("a German reader is shown %q where nothing German was written; "+
			"the banner should still be the one that exists", got)
	}
}

// The wizard asks for the instance's name in both languages.
//
// It is the first thing an administrator meets and the one step that exists to
// name the installation - so a company working in two languages should not have
// to come back to the appearance screen afterwards to say it a second time.
//
// Two boxes here rather than the switcher the appearance screen has: two is small
// enough to put on a wizard step, and four texts in two languages is not.
func TestTheWizardTakesTheTitleInBothLanguages(t *testing.T) {
	p := open(t)

	p.signIn(harness.AdminEmail, harness.AdminPassword)
	p.waitGone("#login-screen")

	p.run("wait for the wizard", chromedp.WaitVisible("#setup-wizard", chromedp.ByID))

	// Straight to the naming step: the ones before it are the database and the
	// password, which have their own cases.
	p.run("skip to the naming step", chromedp.Evaluate(
		`(() => {
			const at = setup.state.steps.findIndex(s => s.id === 'branding');
			if (at < 0) return 'no naming step';

			setup.index = at;
			renderSetup();

			return 'ok';
		})()`, nil))

	p.run("wait for the fields",
		chromedp.WaitVisible(`#setup-step-fields input[name="title.en"]`, chromedp.ByQuery))

	if p.count(`#setup-step-fields input[name="title.de"]`) != 1 {
		t.Fatal("the naming step asks for one language only")
	}

	p.run("name it twice",
		chromedp.SetValue(`#setup-step-fields input[name="title.en"]`, "Time Recording GmbH",
			chromedp.ByQuery),
		chromedp.SetValue(`#setup-step-fields input[name="title.de"]`, "Zeiterfassung GmbH",
			chromedp.ByQuery),
		p.click("#setup-next"))

	// Stored, and told apart by language: the reader is on English here.
	p.waitForText("#app-title", "Time Recording GmbH")

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText("#app-title", "Zeiterfassung GmbH")
}
