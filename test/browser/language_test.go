//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The language switcher decides, and the browser decides when it has not.
//
// The rule for everything to do with language, and this is the half that is easy
// to get subtly wrong: which *dictionary* to read has to collapse to a language
// this application ships words for, and how to write a *date* must not. There is
// one English dictionary and there is not one English date - reduced to plain
// "en", an en-GB browser is formatted in the American order, so the twelfth of
// August reads as 08/12/2026.
//
// Asserted against the browser's own Intl rather than against a hard-coded
// string: what is being checked is that the page agrees with the reader's
// machine, and hard-coding a format would only assert what CI's Chrome happens
// to be set to.
func TestTheChosenLanguageWinsAndTheBrowserDecidesOtherwise(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Nothing chosen yet: the built-in administrator's account carries no
	// language until something stores one, and adoption only runs for an
	// ordinary first sign-in.
	var same bool

	p.run("the browser decides", chromedp.Evaluate(`
		(() => {
			const wanted = navigator.languages?.[0] ?? navigator.language;
			const on = new Date(Date.UTC(2026, 7, 12));
			const shape = { day: '2-digit', month: '2-digit', year: 'numeric', timeZone: 'UTC' };

			return new Intl.DateTimeFormat(activeLocale(), shape).format(on)
				=== new Intl.DateTimeFormat(wanted, shape).format(on);
		})()`, &same))

	if !same {
		t.Error("with no language chosen, dates are not written the way the reader's " +
			"own browser writes them")
	}

	// And a choice that genuinely disagrees wins outright - picking one language
	// on a browser asking for another is a decision, and a date is part of how an
	// application reads.
	var chosen string

	p.run("a chosen language wins", chromedp.Evaluate(`
		(() => {
			const other = (navigator.languages?.[0] ?? 'en').toLowerCase().startsWith('de')
				? 'en' : 'de';

			me.user = { ...(me.user ?? {}), language: other };
			const got = activeLocale();
			delete me.user.language;

			return got + '|' + other;
		})()`, &chosen))

	parts := strings.Split(chosen, "|")
	if len(parts) != 2 || parts[0] != parts[1] {
		t.Errorf("a chosen language did not win: activeLocale gave %q for a chosen %v",
			parts[0], parts[1:])
	}

	// The dictionary still collapses to a language there are words for, or every
	// key would fall through to English for any browser with a region on its tag.
	var known bool

	p.run("the dictionary is keyed on a language", chromedp.Evaluate(
		`['de', 'en'].includes(activeLanguage())`, &known))

	if !known {
		t.Errorf("activeLanguage() answered something the dictionary is not keyed on")
	}
}

// Switching the language reaches what is already on screen.
//
// Most of the interface is markup, and applyLanguage translates that. What it did
// not reach was everything the script had already written into the page - and
// those are precisely the screens somebody has to ask for: an evaluation, an
// overtime balance, an import preview. Drawn once, from an answer that arrived
// once, and never looked up again.
//
// So a screen ended up half translated in a way that reads as a bug in the
// translations rather than in the redrawing: the table heading said "Zeitraum"
// above a cell saying 07/14/2026, beside a total reading "5.01 h in total". Every
// key involved had a German entry. None of them was asked for a second time.
//
// Only a browser can show this. It is not what the strings are, it is when they
// were looked up.
func TestSwitchingLanguageRedrawsWhatIsAlreadyOnScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("book an hour", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "5.01",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "5.01")

	p.run("evaluate", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-report", chromedp.ByID),
		p.click(`#form-report button[type="submit"]`),
		chromedp.WaitVisible("#report-result", chromedp.ByID))

	// English first, so the change below is a change rather than the starting
	// state. The suite pins the browser to en-US and this account has chosen
	// nothing, so this is what it renders in.
	if total := p.text("#report-total"); !strings.Contains(total, "in total") {
		t.Fatalf("the total reads %q before the switch, which is not English - this "+
			"case cannot tell a redraw from the starting state", total)
	}

	p.chooseLanguage("de")

	// The heading is markup and was always translated; the total is the script's
	// and was not. Both are checked, so a redraw that somehow lost the markup
	// would not pass either.
	p.waitForText("#table-report thead", "Zeitraum")

	if total := p.text("#report-total"); !strings.Contains(total, "gesamt") {
		t.Errorf("the total still reads %q after switching to German", total)
	}

	// The figure with it: a German reader writes 5,01 rather than 5.01, and that
	// is drawn by the same call that writes the words around it.
	if total := p.text("#report-total"); !strings.Contains(total, "5,01") {
		t.Errorf("the total reads %q, which is not a German figure", total)
	}

	// The date in the table, which was showing the American order on a German
	// screen.
	if period := p.text("#table-report tbody td"); strings.Contains(period, "/") {
		t.Errorf("the period reads %q, which is still not written the German way",
			period)
	}

	// And the chart beside it, whose caption and labels were translated when the
	// answer arrived rather than when it is drawn.
	if caption := p.text("#report-chart-caption"); !strings.Contains(caption, "Stunden") {
		t.Errorf("the chart caption still reads %q", caption)
	}
}

// Nothing that has a German entry is still showing its English source.
//
// The dictionary tests prove every key has a translation. They cannot prove the
// translation reached the page, and those are different failures: a card built by
// the script after applyLanguage has already run keeps its English until
// something runs it again, and a screen drawn from an answer that arrived earlier
// keeps whatever it was drawn with.
//
// So this asks the page itself, in German, across every view: for each element
// carrying a data-i18n whose key the German dictionary has, is the text on screen
// the German one? That is the whole question, asked as a property rather than as a
// list of words somebody remembered to look for.
func TestNothingOnScreenKeepsItsEnglishWhenGermanIsChosen(t *testing.T) {
	t.Parallel()

	// Both kinds of account, because they see different halves of the application
	// and neither half is the whole of it: the administrator has the settings and
	// the spreadsheet cards, and somebody who works here has the time, the
	// calendar, the evaluation and the overtime balance - which is where this was
	// reported.
	t.Run("administrator", func(t *testing.T) {
		checkGermanEverywhere(t, func(p *page) { p.readyAdmin() })
	})

	t.Run("works here", func(t *testing.T) {
		checkGermanEverywhere(t, func(p *page) { p.readyWorker() })
	})
}

func checkGermanEverywhere(t *testing.T, signIn func(*page)) {
	t.Helper()

	p := open(t)
	signIn(p)

	p.chooseLanguage("de")

	p.waitForText("#tabs", "Einstellungen")

	// Every view, because a tab nobody opened is a tab nobody looked at - and the
	// spreadsheet cards in particular are built by the script rather than written
	// in the markup.
	var views []string

	p.run("list the tabs", chromedp.Evaluate(
		`Array.from(document.querySelectorAll('.tab[data-view]'))
			.filter(t => !t.hidden).map(t => t.dataset.view)`, &views))

	if len(views) < 3 {
		t.Fatalf("only %d tabs are open to this account; this proves little", len(views))
	}

	for _, view := range views {
		p.run("open "+view, p.click(`.tab[data-view="`+view+`"]`),
			chromedp.WaitVisible(`#view-`+view, chromedp.ByID))

		var untranslated []string

		p.run("check "+view, chromedp.Evaluate(`
			(() => {
				const german = TRANSLATIONS.de ?? {};
				const bad = [];

				for (const node of document.querySelectorAll('[data-i18n]')) {
					if (!node.offsetParent && node.offsetHeight === 0) continue;

					const wanted = german[node.dataset.i18n];
					if (wanted === undefined) continue;

					// Labels wrap their input, so the text is the first text node -
					// textContent would drag the field's own value in with it.
					const first = node.firstChild;
					const shown = first && first.nodeType === Node.TEXT_NODE
						? first.nodeValue : node.textContent;

					if (shown.trim() !== wanted.trim()) {
						bad.push(node.dataset.i18n + ': shows ' + JSON.stringify(shown.trim().slice(0, 60)));
					}
				}

				return bad;
			})()`, &untranslated))

		for _, one := range untranslated {
			t.Errorf("%s: %s", view, one)
		}
	}
}

// Every marked tab carries exactly one mark, and says nothing extra about it.
//
// Two things can go wrong here and neither shows up by looking. A mark placed
// before the label is wiped by the language switch, which replaces the first
// text node of a translated element - so the marks vanish the moment somebody
// switches to German. And a mark that is not hidden from assistive technology
// is announced beside the label, which turns "Calendar" into "image, Calendar".
func TestTheMarkedTabsKeepTheirMarksInEveryLanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Asked of the navigation rather than of a list written here, so a tab added
	// later without a mark fails this rather than quietly standing out.
	unmarked := func() string {
		var out string

		p.run("look for bare tabs", chromedp.Evaluate(`
			[...document.querySelectorAll('#tabs .tab')]
				.filter((tab) => tab.querySelectorAll('.tab-icon').length !== 1)
				.map((tab) => tab.dataset.view)
				.join(', ')`, &out))

		return out
	}

	if bare := unmarked(); bare != "" {
		t.Errorf("these tabs do not carry exactly one mark: %s", bare)
	}

	// Hidden from a screen reader, which reads the label instead.
	var announced int

	p.evalJSON(`JSON.stringify(
		[...document.querySelectorAll('#tabs .tab-icon')]
			.filter((mark) => mark.getAttribute('aria-hidden') !== 'true').length)`,
		&announced)

	if announced != 0 {
		t.Errorf("%d marks are announced beside the label they already have", announced)
	}

	// And the label is still the label. This is what breaks if a mark is put
	// before the words rather than after them.
	before := strings.TrimSpace(p.text(`.tab[data-view="calendar"]`))

	p.chooseLanguage("de")
	p.waitForText(`.tab[data-view="calendar"]`, "Kalender")

	if bare := unmarked(); bare != "" {
		t.Errorf("after switching language these tabs lost their mark: %s", bare)
	}

	if after := strings.TrimSpace(p.text(`.tab[data-view="calendar"]`)); after == before {
		t.Errorf("the calendar tab still says %q in German", after)
	}
}

// What somebody has typed is still there after they choose a language.
//
// Choosing a language saves it to the account and reloads every screen, and
// every reload refilled every form from the server on the way past. An
// administrator half way through a database connection who switched to German
// to read a label got the stored connection back over the one they were
// entering - and on an installation with nothing stored, got nothing back at
// all, which looks like a form that was never filled in rather than one that
// was emptied.
//
// The connection card because that is where it was reported, but the rule is
// the same on every card the administration screen refills.
func TestWhatSomebodyTypedSurvivesChoosingALanguage(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	// The card has to have been filled before anything is typed into it, or the
	// answer arrives afterwards and this proves nothing about reloads.
	p.waitForFilled("#datasource-active")

	// A server dialect, so the fields below the type are on screen at all.
	//
	// Picked with the keyboard rather than assigned, for the same reason the
	// boxes below are typed into rather than assigned - and the distinction is
	// sharper here than it was. The type follows what this installation is
	// connected to until somebody chooses otherwise, because it decides which
	// other fields exist and a stale one makes the whole card describe a
	// connection that is not there. A value set from a script, events and all,
	// is what restoring a draft does; only a real press is a choice.
	p.run("choose postgres",
		chromedp.Focus(`#form-datasource select[name="dialect"]`, chromedp.ByQuery),
		chromedp.SendKeys(`#form-datasource select[name="dialect"]`, "p",
			chromedp.ByQuery))

	if got := p.value(`#form-datasource select[name="dialect"]`); got != "postgres" {
		t.Fatalf("the keyboard did not pick PostgreSQL; the type reads %q", got)
	}

	// Typed rather than assigned. Assigning a value fires no events, and events
	// are how the page knows somebody is part way through - which is the whole
	// distinction being tested.
	typed := map[string]string{
		"host": "db.example.invalid", "name": "gtr_live", "user": "gtr_admin",
	}

	for field, value := range typed {
		p.run("type the "+field, chromedp.SendKeys(
			`#form-datasource [name="`+field+`"]`, value, chromedp.ByQuery))
	}

	// The switch, and the reload it starts, in full.
	p.chooseLanguage("de")

	for field, value := range typed {
		got := p.value(`#form-datasource [name="` + field + `"]`)

		if got != value {
			t.Errorf("the %s field reads %q after a language was chosen; %q was typed "+
				"into it and never saved", field, got, value)
		}
	}

	// And the type as well, which is a picker rather than a box.
	if got := p.value(`#form-datasource select[name="dialect"]`); got != "postgres" {
		t.Errorf("the type reads %q after a language was chosen; postgres was chosen", got)
	}
}

// A name saved is the name the language chooser offers a moment later.
//
// Saving the appearance answers, and the reload that brings the screen up to
// date runs behind the notice saying it worked. The copy the language chooser
// fills its boxes from was part of that reload - so between the notice and the
// reload, choosing a language filled the boxes with the name from before the
// save. Saving that stored the old name as a translation of the new one: a
// wrong value, written by a screen that looked like it was showing the right
// one.
//
// Timed off the notice itself rather than raced against it. The switch happens
// inside the observer that sees the notice appear, which is the same task the
// notice is added in - so nothing the reload answers can have landed yet, and
// this either passes because the copy was already right or fails every time.
func TestTheNameJustSavedIsWhatTheLanguageChooserOffers(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.saveBranding(t, "name the installation",
		chromedp.SetValue(`#form-branding input[name="title"]`, "Alpha GmbH", chromedp.ByQuery))

	p.run("watch for the notice", chromedp.Evaluate(`
		(() => {
			window.__offered = null;

			const notice = document.querySelector('#toast');

			const watching = new MutationObserver(() => {
				if (!/saved|gespeichert/i.test(notice.textContent)) return;

				watching.disconnect();

				const picker = document.querySelector('#branding-language');
				picker.value = 'de';
				picker.dispatchEvent(new Event('change'));

				window.__offered = document.querySelector('#form-branding').elements.title.value;
			});

			watching.observe(notice, { childList: true, subtree: true, characterData: true });

			return 1;
		})()`, nil))

	p.saveBranding(t, "rename it",
		chromedp.SetValue(`#form-branding input[name="title"]`, "Beta GmbH", chromedp.ByQuery))

	p.waitEvaluates("what German was offered",
		`String(window.__offered ?? "")`, "Beta GmbH")
}

// An account that has never chosen a language is given one, and shows it.
//
// The picker offers the languages this interface speaks and nothing else, so an
// account with none stored is not one that chose to follow the browser - it is
// one nobody has decided for yet, and there is no way back to that state from
// the screen. It used to stay that way whenever the one-time adoption had been
// missed, and the topbar then carried a select set to a value no option holds,
// which a browser draws as an empty box: a language that looks like it could
// not be worked out rather than one that simply was not stored.
//
// The account is created and signed into here rather than using the built-in
// administrator, because the administrator's first sign-in is spent on the
// wizard and the password, and this is about the ordinary way in.
func TestAnAccountWithNoLanguageIsGivenOneAndTheTopbarSaysWhich(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	const (
		email    = "sprachlos@example.com"
		password = "sprachlos-password-1"
	)

	p.createOrdinaryAccount(t, email, password)

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(email, password)
	p.waitGone("#login-screen")
	p.settleWelcome()
	p.settled()

	// Read from the server, because the point is that it was written down.
	if stored := p.storedAccount(t).Language; stored == "" {
		t.Error("the account still has no language after signing in; there is no " +
			"way to choose that state and no way out of it")
	}

	// And the control says which. Not which language - that is the machine
	// running this - but that it names one at all.
	if shown := p.value("#language-picker"); shown == "" {
		t.Error("the language picker shows nothing; a select holding a value no " +
			"option carries is drawn as an empty box")
	}

	// The two agree, which is the whole of what this control is for.
	if shown, stored := p.value("#language-picker"), p.storedAccount(t).Language; shown != stored {
		t.Errorf("the picker shows %q and the account holds %q", shown, stored)
	}

	// And the same control asked about an account with nothing stored, which is
	// what every account is until the line above has run.
	//
	// Asked of the function rather than of a sign-in, because the state cannot be
	// reached any other way: the server refuses to store an empty language, so
	// there is no request that produces one. What there is, is a moment - between
	// signing in and the adoption landing, or after an adoption that failed - and
	// in that moment this control was blank.
	var shown string

	p.run("fill the picker for an account with nothing stored", chromedp.Evaluate(`
		(async () => {
			me.user.language = '';
			await loadLanguages();

			return document.querySelector('#language-picker').value;
		})()`, &shown, awaitPromise))

	if shown == "" {
		t.Error("the picker is blank for an account with no language stored; it " +
			"should name the one the page is actually reading in")
	}
}

// A language adoption that was missed once is tried again.
//
// Both the zone and the language used to be written behind one marker in this
// browser's storage, recorded per account and never cleared. That is right for
// the zone: an empty stored zone means "follow the instance", a choice somebody
// can make and see offered, so adopting the browser's on every load would make
// it impossible to keep.
//
// It is wrong for the language, which has no such choice to protect. Under the
// marker, an adoption that did not happen the first time - storage refused, the
// request failed, or the marker was already there from a session before the
// account had one - left the account with no language for ever, and its picker
// blank, with nothing on screen able to put it right.
//
// Asked by watching for the request rather than by reading the account back:
// the server refuses to store an empty language, so the account cannot be put
// into that state to be rescued from it. What can be checked is that finding it
// undecided is enough to decide it.
func TestALanguageAdoptionThatWasMissedIsTriedAgain(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	var asked string

	p.run("adopt with the marker already recorded", chromedp.Evaluate(`
		(async () => {
			const real = window.fetch;
			let seen = false;

			window.fetch = (...args) => {
				const url = typeof args[0] === 'string' ? args[0] : args[0]?.url ?? '';
				if (String(url).includes('/me/language')) seen = true;

				return real(...args);
			};

			try {
				// Both of the conditions the old arrangement gave up on.
				localStorage.setItem('gtr.adopted.' + me.user.id, '1');
				me.user.language = '';

				await adoptBrowserDefaults();
			} finally {
				window.fetch = real;
			}

			return String(seen);
		})()`, &asked, awaitPromise))

	if asked != "true" {
		t.Error("an account with no language was left without one because this " +
			"browser had already recorded the zone for it; there is no way back " +
			"to that state from the screen and no way out of it")
	}
}

// The appearance card comes back with each language's words under that language.
//
// It keeps more than its boxes. The texts exist once per language and only the
// chosen language's are on screen; the rest are held in memory while the card is
// open. A draft made of the boxes alone would come back as the right words filed
// under the wrong language, which is worse than losing them - it is a wrong
// translation that nobody typed, waiting to be saved by somebody who thinks they
// are looking at what they wrote.
func TestTheAppearanceCardKeepsEachLanguagesWordsAcrossAReload(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))
	p.settled()

	const (
		base   = "Alpha GmbH"
		german = "Alpha GmbH (auf Deutsch)"
	)

	p.run("name the installation", chromedp.SetValue(
		`#form-branding input[name="title"]`, base, chromedp.ByQuery))

	p.run("look at German", p.chooseOption("#branding-language", "de"))

	p.run("write a German name", chromedp.SetValue(
		`#form-branding input[name="title"]`, german, chromedp.ByQuery))

	// Nothing was saved. Everything above is unfinished work.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settleWizard()
	p.settled()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	if shown := p.value("#branding-language"); shown != "de" {
		t.Errorf("the card came back showing %q; German was what was open", shown)
	}

	if got := p.value(`#form-branding input[name="title"]`); got != german {
		t.Errorf("the German name reads %q after a reload; %q was written", got, german)
	}

	// And the base is still the base, which is the half that would be silently
	// wrong if the boxes were all that had been kept.
	p.run("back to the base", p.chooseOption("#branding-language", "en"))

	if got := p.value(`#form-branding input[name="title"]`); got != base {
		t.Errorf("the base name reads %q after a reload; %q was written", got, base)
	}
}

// English is the source language and German is a dictionary over it. If the
// dictionary is not applied, the page stays English - which looks fine and is
// wrong.
func TestSwitchingLanguageTranslatesThePage(t *testing.T) {
	t.Parallel()

	p := open(t)

	// The starting language is whatever the browser asks for - that is the
	// point of the auto-detection - so it is set explicitly rather than
	// assumed. A German Windows made this test fail by being right.
	p.run("start from English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	english := p.text(`label[data-i18n="login.email"]`)
	if english == "" {
		t.Fatal("the sign-in form should have a labelled email field")
	}

	p.run("switch to German", chromedp.Evaluate(`applyLanguage('de')`, nil))
	german := p.waitChanged(`label[data-i18n="login.email"]`, english)
	if german == english {
		t.Errorf("the label did not change when switching language (still %q)", english)
	}

	// Back to English restores the markup's own text, which is what an
	// untranslated key falls back to.
	p.run("switch back to English", chromedp.Evaluate(`applyLanguage('en')`, nil))
	if back := p.waitChanged(`label[data-i18n="login.email"]`, german); back != english {
		t.Errorf("expected %q back, got %q", english, back)
	}
}
