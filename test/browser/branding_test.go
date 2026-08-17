//go:build browser

package browser

import (
	"encoding/json"
	"fmt"
	"math"
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
// wideLogo is the shape installations actually upload: a wordmark far wider than
// it is tall, which is what makes the part shown in a square tab a question.
const wideLogo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

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

// The two places a company's own mark goes stay empty until it puts one there.
//
// They fell back to the shipped mark for a while, on the reasoning that a header
// of words alone looks unfinished. It is the wrong place for it: filling the
// slots meant for whoever runs the installation makes an unbranded one look
// branded by somebody else. The application's own mark has its own places - the
// browser tab, and the button beside the title - which say which program this is
// without taking the space meant for the company.
func TestTheLogoSlotsStayEmptyUntilALogoIsConfigured(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Nothing configured: this instance has never been given a logo.
	//
	// Asked of the source rather than of visibility alone, because the sign-in
	// screen is hidden while somebody is signed in - what matters is that there
	// is nothing waiting in it for the next visitor.
	for _, holder := range []string{"#brand-logo", "#login-logo"} {
		if src := p.attr(holder, "src"); src != "" {
			t.Errorf("%s shows %.40q on an installation with no logo", holder, src)
		}
	}

	if p.visible("#brand-logo") {
		t.Error("the header shows a mark on an installation that has configured none")
	}

	// The application's own mark is there, though, beside the title. It is what
	// the browser tab shows too, so the two read as one program.
	if !p.visible(".app-mark") {
		t.Error("the welcome button has no mark, so nothing on the screen says " +
			"which application this is")
	}

	// Drawn, not typed. The house character it replaced is a font glyph: a
	// different picture on every platform and a hollow box where the font has no
	// house in it.
	var marks int

	p.evalJSON(`JSON.stringify(document.querySelectorAll('.app-mark svg, svg.app-mark').length)`,
		&marks)

	if marks == 0 {
		t.Error("the mark beside the title is not drawn")
	}

	// And a configured logo fills the header.
	const logo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))

	if src := p.attr("#brand-logo", "src"); !strings.HasPrefix(src, "data:image/") {
		t.Errorf("the header shows %.40q after a logo was configured", src)
	}

	if !p.visible("#brand-logo") {
		t.Error("the header's mark is hidden after a logo was configured")
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

// Each place can be given a different part of the logo.
//
// A logo is uploaded once and drawn in three places that want three different
// things. A wide header takes the whole wordmark; a browser tab cannot - sixteen
// pixels of a two-to-one wordmark is a smear, and what is worth keeping there is
// usually the mark at one end. Nobody can guess which end, so it is chosen.
//
// The selection keeps the shape of the place it is for, so what is on this screen
// is what will be on the others: a free-form one would be a selection that cannot
// be used as chosen.
func TestAPartOfTheLogoCanBeChosenForEachPlace(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// The previews are controls, and each one opens the chooser for its own place.
	if p.count(".logo-use-button") != 3 {
		t.Fatalf("%d previews can be pressed, want one per place",
			p.count(".logo-use-button"))
	}

	p.run("choose the part used in the tab",
		p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))

	// Square, because a tab is. The box is the application's answer to "as much
	// of this as a square can hold", before anything is dragged.
	var box struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const r = document.querySelector('#crop-box').getBoundingClientRect();
		return { width: r.width, height: r.height };
	})())`, &box)

	if box.Width < 8 || box.Height < 8 {
		t.Fatalf("the selection opened at %.0fx%.0f, which cannot be aimed at",
			box.Width, box.Height)
	}

	if ratio := box.Width / box.Height; ratio < 0.9 || ratio > 1.1 {
		t.Errorf("the tab's selection is %.0fx%.0f, a ratio of %.2f - a tab is square",
			box.Width, box.Height, ratio)
	}

	// Moved to the left end of the logo, which is where a wordmark keeps its
	// mark, and stored.
	p.run("take the left end", chromedp.Evaluate(
		`(() => {
			cropBox = { ...cropBox, x: 0, y: 0 };
			drawCropBox();
			document.querySelector('#crop-apply').click();

			return 1;
		})()`, nil))

	// What the page believes it chose, before saving - so a failure says which of
	// the two halves broke rather than only that the answer is empty.
	var chosen string

	p.run("read the chosen part", chromedp.Evaluate(
		`JSON.stringify(logoCrops)`, &chosen))

	if !strings.Contains(chosen, "icon") {
		t.Fatalf("the chooser stored nothing for the tab: %s", chosen)
	}

	// What the request actually carried, recorded as it goes: the page is where
	// this was failing, and the body it sends is the one thing neither side's log
	// shows.
	// Kept in the session rather than on the window: the save reloads the page,
	// and everything this document was holding goes with it.
	p.run("watch the save", chromedp.Evaluate(`
		(() => {
			sessionStorage.removeItem('__sent');
			const real = window.fetch;

			window.fetch = (url, options) => {
				if (String(url).includes('/settings/branding')) {
					sessionStorage.setItem('__sent', options?.body ?? '');
				}

				return real(url, options);
			};

			return 1;
		})()`, nil))

	p.submitAndAwaitReload(t, p.click(`#form-branding button[type="submit"]`))

	var sent string

	p.run("read what was sent", chromedp.Evaluate(
		`String(sessionStorage.getItem('__sent') ?? 'nothing was sent')`, &sent))

	if !strings.Contains(sent, "crops") {
		t.Fatalf("the save carried no crops: %.300s", sent)
	}

	// What was chosen comes back, so the chooser reopens on it rather than
	// starting again - and the server has it, which is what the three sizes are
	// made from.
	var stored struct {
		Icon struct {
			X float64 `json:"x"`
			W float64 `json:"w"`
		} `json:"icon"`
	}

	var raw string

	p.run("read what was stored", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/branding', { credentials: 'same-origin' });
			const body = await r.json();

			return JSON.stringify(body?.data?.crops ?? {});
		})()`, &raw, awaitPromise))

	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("reading what was stored: %v; %s", err, raw)
	}

	if stored.Icon.W <= 0 {
		t.Fatal("nothing was stored for the tab's part of the logo")
	}

	if stored.Icon.W >= 1 {
		t.Errorf("the whole logo was stored (w=%.2f) rather than the part chosen",
			stored.Icon.W)
	}

	if stored.Icon.X > 0.05 {
		t.Errorf("the part stored starts at %.2f rather than at the left end",
			stored.Icon.X)
	}
}

// The selection is free: a corner pulled in one direction moves in that
// direction only, and whatever shape comes out of it is what gets stored.
//
// The opening selection has the shape of the place it is for, and that used to
// be a rule rather than a starting point - a corner dragged upwards took the
// width with it, so a tab could only ever be given a square of the logo. This is
// the test that the rule is gone, dragged with real pointer input so that a
// corner nothing is wired to fails here rather than passing.
func TestTheChosenPartCanBeAnyShape(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("choose the part used in the tab",
		p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))

	before := p.selection(t)

	// The logo is five times as wide as it is tall, so the square the tab opens
	// on is the full height of it - which is what makes pulling the bottom edge
	// up a fair question to ask.
	if before.H < 0.9 {
		t.Fatalf("the tab's selection opened %.2f of the way down the logo, "+
			"so there is nothing to pull up", before.H)
	}

	p.drag(`.crop-handle-se`, 0, -60)

	after := p.selection(t)

	if after.H >= before.H-0.1 {
		t.Fatalf("the bottom edge was pulled up and the selection is still "+
			"%.2f tall, was %.2f", after.H, before.H)
	}

	// The point of the whole exercise. Under the old rule this width would have
	// followed the height down to keep the square square.
	if math.Abs(after.W-before.W) > 0.02 {
		t.Errorf("pulling the bottom edge up changed the width from %.2f to %.2f; "+
			"a corner should move the two edges it is dragged along and no others",
			before.W, after.W)
	}

	// Still on the logo. A selection that has been dragged off it describes a
	// part that does not exist, which the server would have to guess about.
	if after.X < 0 || after.Y < 0 || after.X+after.W > 1.001 || after.Y+after.H > 1.001 {
		t.Errorf("the selection left the logo: %+v", after)
	}

	p.run("use this part", p.click("#crop-apply"))
	p.submitAndAwaitReload(t, p.click(`#form-branding button[type="submit"]`))

	// And it survives being stored: the shape that was chosen is the shape that
	// comes back, rather than one the server rounded to something it preferred.
	var stored struct {
		Icon struct {
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"icon"`
	}

	var raw string

	p.run("read what was stored", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/branding', { credentials: 'same-origin' });
			const body = await r.json();

			return JSON.stringify(body?.data?.crops ?? {});
		})()`, &raw, awaitPromise))

	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("reading what was stored: %v; %s", err, raw)
	}

	if math.Abs(stored.Icon.H-after.H) > 0.02 || math.Abs(stored.Icon.W-after.W) > 0.02 {
		t.Errorf("chose %.2fx%.2f and %.2fx%.2f came back",
			after.W, after.H, stored.Icon.W, stored.Icon.H)
	}
}

// selection is the part of the logo the chooser currently has marked, in
// fractions of the whole image.
func (p *page) selection(t *testing.T) struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
} {
	t.Helper()

	var box struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}

	// Read off the screen rather than out of the page's own variable: what is
	// drawn is what is being aimed at, and the two agreeing is part of what is
	// under test.
	p.evalJSON(`JSON.stringify((() => {
		const image = document.querySelector('#crop-image');
		const area = drawnImageArea(image);
		const box = document.querySelector('#crop-box').getBoundingClientRect();
		const stage = document.querySelector('#crop-stage').getBoundingClientRect();

		return {
			x: (box.left - stage.left - area.left) / area.width,
			y: (box.top - stage.top - area.top) / area.height,
			w: box.width / area.width,
			h: box.height / area.height,
		};
	})())`, &box)

	return box
}

// The tab can be named separately from the header.
//
// They were one field, which holds until the header's name is too long to be a
// tab: a browser gives a tab a couple of dozen characters and somebody has six
// of them open, so "Zeiterfassung der Beispiel GmbH & Co. KG" reads as
// "Zeiterfassung der B…" in every one of them.
func TestTheBrowserTabCanBeNamedSeparately(t *testing.T) {
	const (
		header = "Zeiterfassung der Beispiel GmbH"
		tab    = "Zeiterfassung"
	)

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("name both",
		chromedp.SetValue(`#form-branding input[name="title"]`, header, chromedp.ByQuery),
		chromedp.SetValue(`#form-branding input[name="tabTitle"]`, tab, chromedp.ByQuery),
		p.click(`#form-branding button[type="submit"]`))

	p.waitForText("#toast", "saved")

	// The two names, in the two places. The header keeps the long one - that is
	// the one place there is room for it.
	var title string

	p.run("read the tab", chromedp.Evaluate(`document.title`, &title))

	if title != tab {
		t.Errorf("the tab is called %q, want %q", title, tab)
	}

	if got := strings.TrimSpace(p.text("#app-title")); got != header {
		t.Errorf("the header says %q, want %q", got, header)
	}

	// And in the document the server writes, which is what the tab reads before
	// any of this application has run. Getting it from the interface alone would
	// leave the tab showing the header's name for as long as the first request
	// takes - which is the flicker this was all built to remove.
	var served string

	p.run("read the served document", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/', { credentials: 'same-origin', cache: 'no-store' });
			const body = await r.text();
			const at = body.indexOf('<title>');

			return body.slice(at, body.indexOf('</title>', at) + 8);
		})()`, &served, awaitPromise))

	if !strings.Contains(served, tab) {
		t.Errorf("the served document's title is %q, without the tab's own name", served)
	}

	if strings.Contains(served, header) {
		t.Errorf("the served document's title is %q, which is the header's name", served)
	}
}

// Saving keeps somebody where they were on the page.
//
// A changed mark reloads - no engine takes a new tab icon from a link swapped in
// afterwards - and the reload used to land at the top. The appearance settings
// are a long way down a long screen, so every save meant scrolling back down to
// see whether what was just saved looks right.
func TestSavingTheLogoKeepsThePlaceOnThePage(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, wideLogo)

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// A different part of the logo for the tab, which is a changed mark and
	// therefore a save that reloads.
	p.run("choose a part", p.click(`.logo-use-button[data-crop="icon"]`),
		chromedp.WaitVisible("#crop-overlay", chromedp.ByID))
	p.drag(`.crop-handle-se`, 0, -40)
	p.run("use it", p.click("#crop-apply"))

	// Somewhere down the page, and submitted without pressing the button: a click
	// scrolls its target into view, which would move the very thing being
	// measured.
	var from float64

	p.evalJSON(`JSON.stringify((() => {
		window.scrollTo(0, 700);

		return window.scrollY;
	})())`, &from)

	if from < 100 {
		t.Skipf("the settings screen is only %.0fpx of scroll here, so there is "+
			"nowhere to be put back to", from)
	}

	p.submitAndAwaitReload(t, chromedp.Evaluate(
		`document.querySelector('#form-branding').requestSubmit()`, nil))

	// Given a moment to be put back: the page grows as each panel answers, and
	// the scroll lands once there is enough page to land on.
	var landed float64

	settled := time.Now().Add(20 * time.Second)

	for {
		p.evalJSON(`JSON.stringify(window.scrollY)`, &landed)

		if math.Abs(landed-from) <= 40 || time.Now().After(settled) {
			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	if math.Abs(landed-from) > 40 {
		t.Errorf("the save left the page at %.0f, having been at %.0f - somebody "+
			"has to scroll back down to what they just changed", landed, from)
	}
}

// submitAndAwaitReload sends the appearance form and waits for the page to come
// back.
//
// A changed mark reloads: no engine takes a new tab icon from a link swapped in
// afterwards, and a differently cropped logo is a different icon. The reload
// takes the saved notice away with it, so waiting for that notice is waiting for
// something that is being removed - which is a wait that sometimes sees it and
// sometimes does not, depending on how quickly the request came back.
//
// The reload itself is what is waited for, by a mark left on the document that
// is about to be replaced.
func (p *page) submitAndAwaitReload(t *testing.T, submit chromedp.Action) {
	t.Helper()

	p.run("mark this document", chromedp.Evaluate(`window.__beforeReload = true`, nil))
	p.run("save", submit)

	deadline := time.Now().Add(30 * time.Second)

	for {
		var gone bool

		p.run("wait for the reload", chromedp.Evaluate(
			`window.__beforeReload === undefined`, &gone))

		if gone {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("the save never reloaded the page; the notice says %q",
				p.text("#toast"))
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.run("wait for the screen", chromedp.WaitVisible("#form-branding", chromedp.ByID))
}

// A language nobody has written anything for goes on following the base.
//
// The form fills every language with the base text, so somebody switching to
// German sees what a German reader currently gets. Saving then stored that as
// German's own answer - so merely looking at a language froze it, and renaming
// the installation afterwards reached every language except the ones that had
// been opened. Same rule as the logo: nothing written means the default applies,
// and goes on applying.
func TestALanguageWithNothingWrittenKeepsFollowingTheBase(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.saveBranding(t, "name the installation",
		chromedp.SetValue(`#form-branding input[name="title"]`, "Alpha GmbH", chromedp.ByQuery),
		chromedp.SetValue(`#form-branding input[name="tabTitle"]`, "Alpha", chromedp.ByQuery))

	// Look at German - which fills the boxes with what a German reader gets
	// today - and save without typing a word.
	p.run("look at German", p.chooseOption("#branding-language", "de"))
	p.saveBranding(t, "save what was only looked at")

	written := p.storedTranslations(t)

	if got := written["de"].Title; got != "" {
		t.Errorf("looking at German stored %q as its own title, so the base no "+
			"longer reaches it", got)
	}

	if got := written["de"].TabTitle; got != "" {
		t.Errorf("looking at German stored %q as its own tab title", got)
	}

	// And the proof that it matters: rename the installation. German has nothing
	// of its own, so it has to follow.
	p.run("back to the base", p.chooseOption("#branding-language", "en"))
	p.saveBranding(t, "rename",
		chromedp.SetValue(`#form-branding input[name="title"]`, "Beta GmbH", chromedp.ByQuery))

	written = p.storedTranslations(t)

	if got := written["de"].Title; got != "" {
		t.Errorf("after the rename German still says %q of its own, so a German "+
			"reader is left on the old name", got)
	}

	if got := written["en"].Title; got != "Beta GmbH" {
		t.Errorf("the base came back as %q after being renamed to Beta GmbH", got)
	}

	// The other half of the rule: a translation somebody actually writes is kept.
	// This must not turn into "translations are never stored".
	p.run("to German", p.chooseOption("#branding-language", "de"))
	p.saveBranding(t, "write a German name",
		chromedp.SetValue(`#form-branding input[name="title"]`, "Beta GmbH (DE)", chromedp.ByQuery))

	if got := p.storedTranslations(t)["de"].Title; got != "Beta GmbH (DE)" {
		t.Errorf("a written German name came back as %q", got)
	}
}

// saveBranding presses Save on the appearance form and waits for that save.
//
// The notices stack, so a "Settings saved" from the previous one is still on
// screen when the next is pressed - and waiting for the word then returns at
// once, before the request has been anywhere near the server. Cleared first, so
// the notice that arrives is the one being waited for.
func (p *page) saveBranding(t *testing.T, what string, fill ...chromedp.Action) {
	t.Helper()

	if len(fill) > 0 {
		p.run(what, fill...)
	}

	p.run("clear the notices", chromedp.Evaluate(
		`(() => { document.querySelector('#toast').replaceChildren(); return 1; })()`, nil))

	p.run("save", p.click(`#form-branding button[type="submit"]`))
	p.waitForText("#toast", "saved")
}

func (p *page) storedTranslations(t *testing.T) map[string]struct {
	Title    string `json:"title"`
	TabTitle string `json:"tabTitle"`
} {
	t.Helper()

	var raw string

	// no-store, because this answer is cacheable and every visitor of the sign-in
	// screen fetches it. Read without it, this returned the copy taken when the
	// page loaded - which made a frozen translation look like an absent one and
	// sent the whole investigation into the server.
	p.run("read the translations", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/branding', { credentials: 'same-origin', cache: 'no-store' });
			const body = await r.json();

			return JSON.stringify(body?.data?.translations ?? {});
		})()`, &raw, awaitPromise))

	written := map[string]struct {
		Title    string `json:"title"`
		TabTitle string `json:"tabTitle"`
	}{}

	if err := json.Unmarshal([]byte(raw), &written); err != nil {
		t.Fatalf("reading the translations: %v; %s", err, raw)
	}

	return written
}
