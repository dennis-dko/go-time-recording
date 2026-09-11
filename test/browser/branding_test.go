//go:build browser

package browser

import (
	"encoding/json"
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
// wideLogo is the shape installations actually upload: a wordmark far wider than
// it is tall, which is what makes the part shown in a square tab a question.
const wideLogo = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAlgAAAB4CAYAAAAuVYzDAAADi0lEQVR4nOzd7U3cQBSGUROlFDqg/xLogF6IIn4ENlrWxq/n655TAPIdCc2j69Xurw0AgCiBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACPvd+wE45/Vtez/7N16et6fM0wAAf7lYJ5MIqkcEFwCc4yKdQIuoukdsAcBxLs+B9QyrW0ILAPZzaQ5opLC6JbQA4DGX5UBGDqtbQgsA7vM1DYOYKa62CZ8XAFoSWAOYNVZmfW4AuJrXPB2tFCheGQLAPzZYnawUV9uC8wDAGQKrg1VjZNW5AOAogdXY6hGy+nwAsIfAaqhKfFSZEwDuEViNVIuOavMCwGcCq4GqsVF1bgAQWAAAYQLrYtW3ONXnB6AmgXUhcfHBOQBQjcC6iKj4ynkAUInAAgAI8/txF7Ctuc9vFhLg/wsYng0WAECYwAqzvfqe8wGgAoEFABAmsIJsZ/ZxTgCsTmABAIQJLACAMIEV4rXXMc4LgJUJLACAMIEFABAmsAAAwgRWgM8T/YxzA2BVAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAgTWAAAYQILACBMYAEAhAksAIAwgQUAECawAADCBBYAQJjAAgAIE1gAAGECK+DleXvq/Qwzcm4ArEpgAQCECSwAgDCBBQAQJrBCfJ7oGOcFwMoEFgBAmMACAAgTWEFee+3jnABYncACAAgTWGG2M99zPgBUILAAAMJsEy7y+ra9936G0dheAVCFDRYAQJjAuohtzVfOA4BKBNaFRMUH5wBANQLrYtXjovr8ANQksAAAwgRWA1W3OFXnBgCB1Ui12Kg2LwB8JrAaqhIdVeYEgHsEVmOrx8fq8wHAHgKrg1UjZNW5AOAogdXJajGy2jwAcIZLcQAz/26hsAKA/9lgDWDWSJn1uQHgagJrELPFymzPCwAtuSQHNPIrQ2EFAI+5LAc2UmgJKwDYz6U5gZ6hJawA4DiX52RaxJaoAoBzXKSTSwSXoAIAAACG5msaAADCBBYAQJjAAgAIE1gAAGECCwAgTGABAIQJLACAMIEFABAmsAAAwgQWAECYwAIACBNYAABhAgsAIExgAQCECSwAgDCBBQAQJrAAAMIEFgBAmMACAAj7EwAA//8NjIji7L4NLAAAAABJRU5ErkJggg=="

func TestAConfiguredTextFillsInPlaceholdersAndMakesLinks(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

	p.chooseLanguage("de")

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
	t.Parallel()

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

	p.chooseLanguage("de")

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
	t.Parallel()

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

	p.chooseLanguage("de")

	p.waitForText("#app-title", "Zeiterfassung GmbH")
}

// The tab can be named separately from the header.
//
// They were one field, which holds until the header's name is too long to be a
// tab: a browser gives a tab a couple of dozen characters and somebody has six
// of them open, so "Zeiterfassung der Beispiel GmbH & Co. KG" reads as
// "Zeiterfassung der B…" in every one of them.
func TestTheBrowserTabCanBeNamedSeparately(t *testing.T) {
	t.Parallel()

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

// A language nobody has written anything for goes on following the base.
//
// The form fills every language with the base text, so somebody switching to
// German sees what a German reader currently gets. Saving then stored that as
// German's own answer - so merely looking at a language froze it, and renaming
// the installation afterwards reached every language except the ones that had
// been opened. Same rule as the logo: nothing written means the default applies,
// and goes on applying.
func TestALanguageWithNothingWrittenKeepsFollowingTheBase(t *testing.T) {
	t.Parallel()

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
//
// And waited past the notice, which is where this used to stop. The notice is
// the answer arriving; the screen is reloaded afterwards, and the copy of the
// appearance the language chooser fills the boxes from is part of what that
// reload replaces. Carrying straight on to another language read the copy taken
// before the save - so looking at German filled the boxes with the name from
// before the rename, and saving that stored it as a German translation. The
// case reported it as a translation appearing out of nowhere, which is exactly
// what it looked like.
func (p *page) saveBranding(t *testing.T, what string, fill ...chromedp.Action) {
	t.Helper()

	if len(fill) > 0 {
		p.run(what, fill...)
	}

	p.run("clear the notices", chromedp.Evaluate(
		`(() => { document.querySelector('#toast').replaceChildren(); return 1; })()`, nil))

	p.run("save", p.click(`#form-branding button[type="submit"]`))
	p.waitForText("#toast", "saved")
	p.atRest()
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
