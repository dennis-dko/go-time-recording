//go:build browser

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Three things that can only be checked in a browser: the log viewer, which is
// polling and filtering and nothing a single request would exercise; whether the
// Settings screen fills every one of its cards, which is a property of the chain
// that loads them rather than of any request in it; and how a passkey interacts
// with two-factor authentication - which needs an actual signature from an actual
// authenticator.

// ---------------------------------------------------------------- live log

// The version belongs in the corner of every page, and the footer used to be
// hidden whenever no branding was configured.
func TestTheFooterShowsTheRunningVersion(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	if !p.visible("#site-footer") {
		t.Fatal("the footer is hidden, so the version has nowhere to appear")
	}

	version := strings.TrimSpace(p.text("#footer-version"))
	if version == "" {
		t.Fatal("the footer shows no version")
	}

	// And the platform beside it, as "v1.2.0 (linux)". The same version is
	// published for four platforms and they do not all behave alike - restarting
	// from the interface works here and cannot on Windows - so the version alone
	// does not say what somebody is looking at.
	//
	// Against the platform this test is running on rather than a hard-coded
	// "linux". CI is Linux and a developer's machine is whatever it is - asserting
	// the former made the suite fail on Windows for saying something true, which
	// trains whoever runs it locally to expect red and stop reading it.
	if !strings.Contains(version, "(") || !strings.Contains(version, ")") {
		t.Errorf("the footer shows %q, without the platform in brackets", version)
	}

	if want := "(" + runtime.GOOS + ")"; !strings.Contains(version, want) {
		t.Errorf("the footer shows %q, want %s - the platform the application is "+
			"actually running on", version, want)
	}

	// The version itself is still in front of it, rather than having been
	// replaced by the platform.
	if strings.HasPrefix(version, "(") {
		t.Errorf("the footer shows only the platform: %q", version)
	}
}

// The viewer has to actually fill with lines, which means the poll ran, the
// response parsed and the rendering worked. A card that stays empty looks
// identical to one that is broken.
func TestTheLogViewerFillsWithLines(t *testing.T) {
	// INFO, or the only lines would be the start-up warnings and there would be
	// nothing to prove polling works.
	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	// The level chips are built from what the server reports, so their presence
	// proves the first request came back.
	p.waitForText("#log-levels", "ERROR")

	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if !strings.Contains(p.text("#log-levels"), level) {
			t.Errorf("no filter offered for %s", level)
		}
	}

	waitForLines(p, "the log viewer never showed a line")

	// Searching narrows what is on screen. The server does the filtering, so this
	// is really asking whether the box reaches it - the filtering itself is
	// covered against the API. Folded in here rather than given its own case
	// because the expensive part is getting to this screen, and a second sign-in
	// and password change is most of a browser test's budget.
	p.run("search for something that cannot appear",
		chromedp.SendKeys("#log-search", "zzz-no-such-line-zzz", chromedp.ByID))

	// The search is debounced and then polled, so this waits rather than
	// asserting at once.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) == "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("a search that matches nothing still shows lines:\n%s",
		truncateText(p.text("#log-output"), 400))
}

// Pausing has to stop the polling, or the button is decoration. Checked by the
// status line, which is what tells the reader whether what they are looking at
// is still moving.
func TestPausingTheLogViewer(t *testing.T) {
	p := openWith(t, "LOG_LEVEL=INFO")
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#log-card", chromedp.ByID))

	waitForLines(p, "the log viewer never showed a line before pausing")

	p.run("pause", p.click("#log-pause"))

	// The button offering to resume is the state, and it is the same assertion in
	// either language the interface ships. Checking the status line's wording
	// would be checking a translation.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		label := strings.ToLower(p.text("#log-pause"))
		if strings.Contains(label, "resume") || strings.Contains(label, "fortsetzen") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Errorf("the pause button still says %q, so nothing was paused", p.text("#log-pause"))
}

// A user must not be offered the log at all - not merely be refused when
// they ask. The whole Settings screen is the built-in administrator's.
func TestAUserIsNotOfferedTheLog(t *testing.T) {
	p := open(t)
	p.readyAdmin()
	p.createOrdinaryAccount(t, "gerd@example.com", "gerd-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("gerd@example.com", "gerd-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	if p.visible("#tab-admin") {
		t.Error("a user is being offered the Settings tab, which holds the log")
	}
}

// The directory synchronisation is not offered to an administrator who may not
// perform one.
//
// Running it deletes every account the directory no longer holds along with
// everything those people recorded, so it belongs to the built-in administrator
// alone - but the Settings tab is opened by anybody holding settings:manage, and
// the card sat in the middle of it with two buttons the server refuses. A
// data-perm cannot express this: it names a permission, and this is about which
// account it is, so only a browser can answer whether the card is there.
func TestAGrantedAdministratorIsNotOfferedTheDirectoryRun(t *testing.T) {
	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "bothe@example.com", "both-jobs-password-1", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("bothe@example.com", "both-jobs-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-ldap", chromedp.ByID))

	if p.visible("#sync-card") {
		t.Error("an administrator who may not synchronise the directory is offered " +
			"the buttons that do it")
	}

	// The connection card above it is theirs to edit, so this is the one card
	// missing rather than the screen having failed to load.
	if !p.visible("#form-ldap") {
		t.Error("the directory connection is missing too, so the screen did not load")
	}
}

// ------------------------------------------------------- the Settings cards

// The Settings screen loads its cards in one unbroken chain of awaits, so a card
// whose request fails does not fail alone: every card after it stays blank while
// the API answers perfectly and nothing else notices. The metrics and tracing
// card went into the middle of that chain, which makes the card after it as much
// the point of this test as the new one is.
func TestTheSettingsScreenFillsTheTelemetryCardAndTheOnesAfterIt(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	// This line is written by the same response that fills the fields, so an
	// empty one means the card never loaded at all.
	active := p.text("#telemetry-active")
	if active == "" {
		t.Error("the telemetry card says nothing about what this process is doing")
	}

	// The metrics endpoint is the one thing on this screen somebody wants to
	// copy, so it has to be there in full rather than as a port number to
	// assemble by hand.
	if !strings.Contains(active, "/metrics") {
		t.Errorf("the metrics endpoint is not named in %q", active)
	}

	// The card after the new one in the chain. Its user filter has a default, so
	// an empty field here means the telemetry request threw and took the rest of
	// the screen down with it.
	if filter := p.value(`#form-ldap input[name="userFilter"]`); filter == "" {
		t.Error("the LDAP card is empty, so loading the cards stopped before it")
	}
}

// The configured logo is on the sign-in screen, not only in the header.
//
// Branding is fetched before anything has authenticated precisely so that this
// screen can carry the instance's own title and logo - somebody arriving at a
// company's time recording should see the company's mark rather than a default.
// Whether the image is actually on screen is a question only a browser answers:
// the element is in the markup, hidden, and it is the script that fills it in and
// unhides it, so nothing short of running the page can tell "wired up" from
// "wired up and working".
func TestTheConfiguredLogoIsOnTheSignInScreen(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// A red rectangle, small enough to be a data URI in a test and unmistakable
	// on a screenshot if this ever needs one.
	// 600x120 - a five-to-one wordmark, which is the shape that makes cropping
	// visible: fitted it stays 5:1, filled it would be cut to the box.
	const logo = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSI2MDAiIGhlaWdodD0iMTIwIj48cmVjdCB3aWR0aD0iNjAwIiBoZWlnaHQ9IjEyMCIgZmlsbD0iIzFmNGU3OSIvPjwvc3ZnPg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	// Through the API rather than the file picker: what is being checked is
	// whether a stored logo reaches the sign-in screen, and driving a file input
	// would be checking the picker instead.
	var status string

	p.run("store a logo", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Zeiterfassung', logo: '`+logo+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" {
		t.Fatalf("could not store the logo: HTTP %s", status)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// Reloaded, because branding is read once at start-up: without this the page
	// still holds what it fetched before the logo existed, and the test would be
	// asserting the state of a screen no visitor ever sees.
	p.run("reload the sign-in screen", chromedp.Reload(),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-logo") {
		t.Error("the configured logo is not shown on the sign-in screen")
	}

	if src := p.attr("#login-logo", "src"); !strings.HasPrefix(src, "data:image/") {
		t.Errorf("the sign-in logo has no image behind it: %.40q", src)
	}

	// And it is given the room a sign-in screen has, rather than the 28px that
	// fits beside the navigation. The same image serves both, so the only thing
	// separating a banner from a favicon-sized mark is what each place allows it.
	var box struct {
		Width   float64 `json:"width"`
		Height  float64 `json:"height"`
		Natural float64 `json:"natural"`
	}

	p.run("measure the banner", chromedp.Evaluate(
		`(() => {
			const el = document.querySelector('#login-logo');
			const r = el.getBoundingClientRect();
			return { width: r.width, height: r.height,
				natural: el.naturalWidth / el.naturalHeight };
		})()`, &box))

	if box.Height <= 40 {
		t.Errorf("the sign-in logo is %.0fpx tall, which is the header's size - it is "+
			"meant to be a banner there", box.Height)
	}

	// Nothing is cut, asked as a property of the image rather than of the fixture:
	// the box it renders in has the shape the image itself has. A box wider or
	// taller than that is a box the image has been filled into, and filling a
	// 5:1 wordmark into anything squarer slices the top and bottom off it.
	//
	// A first version compared the box against a hard-coded 3:1 and passed with
	// object-fit: cover, which crops - the box was still 328x96, because the box
	// is what CSS said and not what was drawn in it.
	if ratio := box.Width / box.Height; ratio < box.Natural*0.95 {
		t.Errorf("the banner renders %.0fx%.0f, a ratio of %.2f, from an image whose "+
			"own ratio is %.2f - it has been cropped to the box rather than fitted "+
			"into it", box.Width, box.Height, ratio, box.Natural)
	}

	// The title travels with it, so a wrong one here would mean branding loaded
	// and only the image failed - a different fault worth telling apart.
	if got := p.text("#login-screen h2"); got == "" {
		t.Error("the sign-in card lost its heading, so this screen did not render")
	}

	// The same image is the tab icon, fetched from the address the server wrote
	// into the document. Asked of what the address answers with rather than of the
	// address itself: what a browser draws in a tab is the bytes, and an href that
	// looks right while serving the shipped mark is exactly the failure this went
	// through twice.
	if !p.iconIsTheLogo(t) {
		t.Error("the browser tab is not served the configured logo")
	}
}

// Clearing the logo puts the shipped mark back in the tab.
//
// The other half, and the half that fails silently: an installation that tries a
// logo and takes it off again would keep the old one for as long as anything
// cached it. The rule asked for was the logo "otherwise always the default as
// before", which is a statement about both directions.
func TestClearingTheLogoRestoresTheDefaultFavicon(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	const logo = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSI2MDAiIGhlaWdodD0iMTIwIj48cmVjdCB3aWR0aD0iNjAwIiBoZWlnaHQ9IjEyMCIgZmlsbD0iIzFmNGU3OSIvPjwvc3ZnPg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)
	p.run("reload with the logo", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if !p.iconIsTheLogo(t) {
		t.Fatal("the logo never reached the tab, so this case cannot show it " +
			"being cleared")
	}

	p.storeBranding(t, "")
	p.run("reload without it", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if p.iconIsTheLogo(t) {
		t.Error("after clearing the logo the tab is still served it")
	}

	// And it is served something rather than nothing: an instance with no logo
	// keeps the shipped mark.
	if kind := p.iconContentType(t); !strings.Contains(kind, "svg") {
		t.Errorf("with no logo the tab icon is served as %q", kind)
	}
}

// iconIsTheLogo fetches whatever the document points its icon at and reports
// whether it is the image this case stored.
//
// The bytes rather than the address. Two attempts at this bug looked correct in
// the DOM and wrong in the tab, and both times the test was reading the element
// that had been changed rather than the picture that had not.
func (p *page) iconIsTheLogo(t *testing.T) bool {
	t.Helper()

	var kind string

	p.run("fetch the tab icon", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return 'no icon declared';

			const r = await fetch(link.href);
			return r.headers.get('content-type') ?? '';
		})()`, &kind, awaitPromise))

	// The fixture is an SVG rectangle; the shipped mark is an SVG too, so the type
	// alone cannot tell them apart - the width can, and it is in the markup the
	// server sends back.
	return strings.Contains(kind, "image/") && p.iconWidth(t) == 600
}

// iconWidth is the intrinsic width of whatever the tab icon is served as.
func (p *page) iconWidth(t *testing.T) int {
	t.Helper()

	var width int

	p.run("measure the tab icon", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return 0;

			return await new Promise((resolve) => {
				const probe = new Image();
				probe.onload = () => resolve(probe.naturalWidth);
				probe.onerror = () => resolve(0);
				probe.src = link.href;
			});
		})()`, &width, awaitPromise))

	return width
}

// iconContentType is what the tab icon is served as.
func (p *page) iconContentType(t *testing.T) string {
	t.Helper()

	var kind string

	p.run("read the icon's type", chromedp.Evaluate(`
		(async () => {
			const link = document.querySelector('link[rel~="icon"]');
			if (!link) return '';

			const r = await fetch(link.href);
			return r.headers.get('content-type') ?? '';
		})()`, &kind, awaitPromise))

	return kind
}

// storeBranding writes the instance's logo through the API, as the Settings form
// does. An empty string clears it.
func (p *page) storeBranding(t *testing.T, logo string) {
	t.Helper()

	var status string

	p.run("store the branding", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/settings/branding', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ title: 'Zeiterfassung', logo: '`+logo+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" {
		t.Fatalf("could not store the branding: HTTP %s", status)
	}
}

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

// ------------------------------------------------------ revealing a password

// The button is added by the script, to fields written in the markup, so
// whether it exists at all is a question only a browser can answer - and what it
// does is a change to an attribute that no API test can see.
func TestAPasswordCanBeRevealedAndHiddenAgain(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	const field = `#form-datasource input[name="password"]`

	// Every password field gets one, so this one stands for the rest.
	if !p.visible(`#form-datasource .password-toggle`) {
		t.Fatal("the database password has no reveal button")
	}

	p.run("type a password", chromedp.SendKeys(field, "not-a-real-password", chromedp.ByQuery))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field starts as %q, want it hidden", got)
	}

	p.run("reveal", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "text" {
		t.Errorf("after pressing the button the field is %q, so nothing was revealed", got)
	}

	// The value survived the type change; a reveal that emptied the field would
	// be worse than none.
	if got := p.value(field); got != "not-a-real-password" {
		t.Errorf("revealing changed the value to %q", got)
	}

	p.run("hide again", p.click(`#form-datasource .password-toggle`))

	if got := p.attr(field, "type"); got != "password" {
		t.Errorf("the field stayed %q after pressing the button a second time", got)
	}
}

// ------------------------------------------------- passkeys and two-factor

// This pins a decision rather than checking a rule, which is why it is worth
// having: with two-factor enabled and a passkey registered, signing in with the
// passkey does not ask for a code.
//
// That is deliberate. Registration and sign-in both demand
// protocol.VerificationRequired, so the device had to see a fingerprint or a PIN
// before it would sign - possession of the device plus verification of the
// person, which is already two factors. It is how Google, Microsoft and Apple
// treat passkeys too: they satisfy multi-factor rather than needing another one
// stacked on top.
//
// The consequence to be aware of: enabling two-factor does not force a second
// factor on someone who has a passkey, because their passkey is a way in that
// never asks. Anyone wanting two-factor as a policy needs more than this
// setting. The test exists so that if the behaviour is ever changed, it is
// changed on purpose.
func TestAPasskeySignsInWithoutATwoFactorCodeEvenWhenTOTPIsOn(t *testing.T) {
	p := open(t)
	p.withAuthenticator(t)

	p.readyAdmin()
	p.createOrdinaryAccount(t, "hanna@example.com", "hanna-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	secret := p.enableTOTP(t)

	// Two-factor really is on: the password alone now stops at the code field.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("hanna@example.com", "hanna-password-1")
	p.run("wait for the code field", chromedp.WaitVisible("#login-totp-field", chromedp.ByID))

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	p.run("supply the code",
		chromedp.SendKeys(`#form-login input[name="totp"]`, code, chromedp.ByQuery),
		p.click(`#form-login button[type="submit"]`))
	p.waitGone("#login-screen")

	// Now register a passkey for the same account.
	p.run("open My account", chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#passkey-card", chromedp.ByID))

	p.run("register a passkey",
		chromedp.SendKeys(`#form-passkey input[name="name"]`, "Hanna's laptop", chromedp.ByQuery),
		p.click(`#form-passkey button[type="submit"]`))

	p.waitForText("#table-passkeys tbody", "Hanna's laptop")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	// And the passkey gets in with no code asked for.
	p.run("sign in with the passkey", p.click("#login-passkey"))
	p.waitGone("#login-screen")

	if p.visible("#login-totp-field") {
		t.Error("the passkey sign-in asked for a two-factor code, which it is not supposed to")
	}

	var who string

	p.run("read who is signed in",
		chromedp.Evaluate(`document.querySelector('#who')?.textContent ?? ''`, &who))

	if !strings.Contains(who, "hanna@example.com") && !strings.Contains(who, "Erika") {
		t.Errorf("expected to be signed in as Hanna, the header says %q", who)
	}
}

// enableTOTP turns two-factor on through the API and returns the secret, so the
// test can produce codes. Driving the enrolment form would be testing the form,
// which other cases already do.
func (p *page) enableTOTP(t *testing.T) string {
	t.Helper()

	// The session first, and waited for rather than assumed.
	//
	// The login screen going away means the sign-in answered, not that every
	// cookie it set is back in the browser and being attached to fetches. Firing
	// straight into an enrolment lost that race about once in a suite run, and
	// then reported it as "no secret was issued" - which describes the symptom and
	// points nowhere near the cause.
	p.waitSignedIn(t)

	var answer struct {
		Status int    `json:"status"`
		Secret string `json:"secret"`
		Body   string `json:"body"`
	}

	p.run("begin two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'POST',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
			});

			const body = await r.text();

			let secret = '';
			try { secret = JSON.parse(body)?.data?.secret ?? ''; } catch {}

			return { status: r.status, secret, body: body.slice(0, 400) };
		})()`, &answer, awaitPromise))

	if answer.Secret == "" {
		t.Fatalf("no two-factor secret was issued: HTTP %d, %s\n\napplication log:\n%s",
			answer.Status, answer.Body, p.app.Log())
	}

	secret := answer.Secret

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatalf("cannot compute a code: %v", err)
	}

	var status string

	p.run("confirm two-factor enrolment", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';

			const r = await fetch('/api/v1/me/totp', {
				method: 'PUT',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ code: '`+code+`' }),
			});

			return String(r.status);
		})()`, &status, awaitPromise))

	if status != "200" && status != "201" {
		t.Fatalf("could not enable two-factor: HTTP %s\n\napplication log:\n%s", status, p.app.Log())
	}

	return secret
}

// ------------------------------------------------------------------ helpers

// waitForLines waits until the log output holds something.
func waitForLines(p *page, complaint string) {
	p.t.Helper()

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(p.text("#log-output")) != "" {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	p.t.Fatalf("%s\n\nstatus: %q\n\napplication log:\n%s",
		complaint, p.text("#log-status"), p.app.Log())
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "…"
}

// ------------------------------------------------------- maintenance mode

// The switch has to work from the screen it lives on, and the notice has to be
// visible afterwards - including on the sign-in screen, which is the only place
// somebody turned away can read anything at all.
func TestTurningMaintenanceModeOnAndOffFromTheInterface(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID))

	p.run("turn it on",
		chromedp.SendKeys(`#form-maintenance input[name="message"]`, "Restoring a backup", chromedp.ByQuery),
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
		// Switching it on asks first. The question is in the page rather than
		// drawn by the browser, so it can simply be answered - a native dialog
		// had to be intercepted, because a headless browser has nobody to click
		// it and an unanswered one blocks every later action.
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery),
	)

	p.run("confirm", p.click(`.confirm-actions button.danger`))

	p.waitForText("#maintenance-banner", "Restoring a backup")

	// Signed out, the notice is what a person sees instead of silence.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#maintenance-banner") {
		t.Error("the notice is not shown on the sign-in screen, where it matters most")
	}

	// And back in, the administrator can end it.
	p.signIn(harness.AdminEmail, adminPassword)
	p.waitGone("#login-screen")
	p.settleWizard()

	p.run("turn it off", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-maintenance", chromedp.ByID),
		p.click(`#form-maintenance input[name="enabled"]`),
		p.click(`#form-maintenance button[type="submit"]`),
	)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !p.visible("#maintenance-banner") {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("the notice is still shown after maintenance mode was turned off: %q",
		p.text("#maintenance-banner"))
}

// ------------------------------------------------- adopting browser defaults

// The browser knows two things the server cannot: which zone the person is
// actually in, and which language they read. Neither was being kept - the
// language was detected for the current page and thrown away on every load, and
// the zone was not detected at all, so somebody far enough east or west saw their
// evening bookings land on the instance's tomorrow until they found the setting.
//
// Only a browser can check this: it is the browser's own zone and language that
// have to reach the database.
func TestAFirstSignInAdoptsTheBrowsersZoneAndLanguage(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// An instance zone that is not this machine's, which is the case the feature
	// exists for - somebody working in a different zone from the installation.
	// The wizard sets the instance to Europe/Berlin, and adopting a zone that
	// already applies would change nothing, so without this the assertion below
	// would be about a no-op.
	p.run("move the instance to another zone", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
			await fetch('/api/v1/settings/timezone', {
				method: 'PUT', credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ timezone: 'Pacific/Auckland' }),
			});
		})()`, nil, awaitPromise))

	p.createOrdinaryAccount(t, "ingrid@example.com", "ingrid-password-1")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("ingrid@example.com", "ingrid-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	// Read back from the server rather than from the form: the point is that it
	// was written to the database, not that a select was filled in.
	stored := p.storedAccount(t)

	if stored.Timezone == "" {
		t.Error("the browser's zone was not adopted, so the account still follows the instance")
	}

	// Whatever the machine running this test is set to - asserting a specific
	// zone would be asserting about the test machine.
	if browser := p.browserTimezone(); stored.Timezone != browser {
		t.Errorf("the account has %q, want the browser's %q", stored.Timezone, browser)
	}

	if stored.Language == "" {
		t.Error("the browser's language was not adopted")
	}

	// And it is a suggestion rather than a standing override: choosing to follow
	// the instance has to survive a reload, or the setting would be unusable.
	p.run("follow the instance again", chromedp.Evaluate(`
		(async () => {
			const csrf = document.cookie.split(';').map(c => c.trim())
				.find(c => c.startsWith('gtr_csrf='))?.slice('gtr_csrf='.length) ?? '';
			await fetch('/api/v1/me/timezone', {
				method: 'PUT', credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
				body: JSON.stringify({ timezone: '' }),
			});
		})()`, nil, awaitPromise))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")

	if again := p.storedAccount(t); again.Timezone != "" {
		t.Errorf("following the instance was overwritten again with %q", again.Timezone)
	}
}

// storedAccount reads /me, so an assertion is about what the server holds rather
// than about what a form shows.
func (p *page) storedAccount(t *testing.T) struct {
	Timezone string
	Language string
} {
	t.Helper()

	var raw string

	p.run("read /me", chromedp.Evaluate(`
		fetch('/api/v1/me', { credentials: 'same-origin' })
			.then(r => r.json())
			.then(b => JSON.stringify({
				timezone: b.data?.user?.timezone ?? '',
				language: b.data?.user?.language ?? '',
			}))`, &raw, awaitPromise))

	var account struct {
		Timezone string `json:"timezone"`
		Language string `json:"language"`
	}

	if err := json.Unmarshal([]byte(raw), &account); err != nil {
		t.Fatalf("cannot read /me: %v (%s)", err, truncateText(raw, 200))
	}

	return struct {
		Timezone string
		Language string
	}{Timezone: account.Timezone, Language: account.Language}
}

// browserTimezone is what the browser reports for itself, which is what the
// adoption is supposed to have stored.
func (p *page) browserTimezone() string {
	p.t.Helper()

	var zone string

	p.run("read the browser zone",
		chromedp.Evaluate(`Intl.DateTimeFormat().resolvedOptions().timeZone || ''`, &zone))

	return zone
}

// ------------------------------------------------------------------- toasts

// A notice raised while the sign-in screen is up used to be invisible.
//
// The toast sat at z-index 20 and the sign-in screen at 30, with an opaque
// background - so a failure during start-up, which is exactly when the sign-in
// screen is still covering the application, was painted behind it. The message
// explaining why nothing worked was the one message nobody could read.
//
// A sign-in that is merely refused is not this case: that message goes into the
// form, next to the field it is about, which is where it belongs.
func TestANoticeIsVisibleOverTheSignInScreen(t *testing.T) {
	p := open(t)

	p.run("wait for the form", chromedp.WaitVisible("#form-login", chromedp.ByID))

	if !p.visible("#login-screen") {
		t.Fatal("the sign-in screen is not up, so this proves nothing")
	}

	p.run("raise a notice", chromedp.Evaluate(`toast('something went wrong', 'error')`, nil))

	// Visible as the browser sees it, which is the whole point: it was present in
	// the markup before this change too, and painted underneath.
	if !p.visible("#toast .toast-note") {
		t.Error("a notice raised while the sign-in screen is up cannot be seen")
	}

	if !strings.Contains(p.text("#toast"), "something went wrong") {
		t.Errorf("the notice says %q", p.text("#toast"))
	}
}

// Two failures in a row used to show only the second, which is the case where
// the first one mattered.
func TestTwoNoticesAreBothShown(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Cleared first: signing in and changing the password raises notices of its
	// own, and they now linger long enough to still be there.
	p.run("raise two notices", chromedp.Evaluate(`
		document.querySelector('#toast').replaceChildren();
		toast('first notice', 'error');
		toast('second notice', 'error');`, nil))

	var count int

	p.run("count the notices",
		chromedp.Evaluate(`document.querySelectorAll('#toast .toast-note').length`, &count))

	if count != 2 {
		t.Errorf("%d notice(s) on screen, want 2", count)
	}

	shown := p.text("#toast")
	for _, want := range []string{"first notice", "second notice"} {
		if !strings.Contains(shown, want) {
			t.Errorf("%q is not shown; the stack says %q", want, shown)
		}
	}
}

// -------------------------------------------------- asking before destroying

// Five delete buttons asked nothing at all - a role, a project, a time entry, a
// token and a passkey all went straight to DELETE on one click. The four that did
// ask used window.confirm, which the browser draws itself: unstyled, naming the
// origin, unreadable in a dark theme and impossible to translate.
//
// Only a browser can check either half: that the question appears, and that
// answering "no" leaves the thing alone.
func TestDeletingAsksFirstAndCancellingChangesNothing(t *testing.T) {
	p := open(t)
	p.readyWorker()

	// A time entry of the administrator's own, so nothing else has to exist.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "1.37", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "1.37")

	// The delete button is a link button in the row's action cell.
	p.run("press delete", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	// The question is ours, not the browser's - so it is in the page and can be
	// seen, which a native dialog cannot be.
	if !p.visible(".confirm-card") {
		t.Fatal("no dialog appeared before deleting")
	}

	// Cancelling has to leave the entry exactly where it was.
	p.run("cancel", p.click(`.confirm-actions button.secondary`))
	p.waitGone(".confirm-overlay")

	if !strings.Contains(p.text("#table-timesheets tbody"), "1.37") {
		t.Error("cancelling the dialog deleted the entry anyway")
	}

	// And confirming deletes it, or the dialog is a wall rather than a question.
	p.run("press delete again", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))
	p.run("confirm", p.click(`.confirm-actions button.danger`))
	p.waitGone(".confirm-overlay")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(p.text("#table-timesheets tbody"), "1.37") {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Error("confirming the dialog did not delete the entry")
}

// Escape is the ambiguous keypress, and a dialog with a destructive option has
// to read it as "no".
func TestEscapeCancelsTheConfirmation(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "2.5", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "2.5")

	p.run("press delete", p.click(`#table-timesheets tbody button.danger`),
		chromedp.WaitVisible(".confirm-overlay", chromedp.ByQuery))

	p.run("press escape", chromedp.KeyEvent("\u001b"))
	p.waitGone(".confirm-overlay")

	if !strings.Contains(p.text("#table-timesheets tbody"), "2.5") {
		t.Error("escape deleted the entry")
	}
}

// ------------------------------------------------------------------ stopwatch

// The clock counts up in the page without asking the server every second, and
// stopping it books what was measured. Both halves are browser behaviour: the
// ticking display, and the buttons swapping over when it starts.
func TestTheStopwatchRunsAndBooksWhatItMeasured(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("open time entries", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-card", chromedp.ByID))

	if !p.visible("#timer-start") {
		t.Fatal("no start button")
	}

	p.run("start it",
		chromedp.SendKeys("#timer-description", "measured in a browser", chromedp.ByID),
		p.click("#timer-start"))

	// The buttons swap over, which is how somebody can tell it took.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !p.visible("#timer-stop") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-stop") {
		t.Fatal("the stop button never appeared, so the clock did not start")
	}

	if p.visible("#timer-start") {
		t.Error("the start button is still offered while the clock runs")
	}

	// And it counts up on its own. Two readings a couple of seconds apart have to
	// differ, or the display is a decoration.
	first := p.text("#timer-elapsed")
	if first == "" {
		t.Fatal("the elapsed display is empty while the clock runs")
	}

	time.Sleep(3 * time.Second)

	if second := p.text("#timer-elapsed"); second == first {
		t.Errorf("the display is stuck at %q, so it is not counting", first)
	}

	// Past the smallest bookable duration - below it the stop is refused on
	// purpose, which is a different test.
	time.Sleep(38 * time.Second)

	p.run("stop and book", p.click("#timer-stop"))

	// The entry appears in the list, with the description that was typed.
	p.waitForText("#table-timesheets tbody", "measured in a browser")

	// And the clock is back to offering a start.
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !p.visible("#timer-start") {
		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#timer-start") {
		t.Error("after stopping, the clock does not offer to start again")
	}
}

// --------------------------------------------------------------------- charts

// The charts are SVG built in the page, because the Content-Security-Policy
// allows no external origin and a chart library from a CDN would simply be
// blocked. That makes them a browser question twice over: whether the elements
// exist, and whether they render - an <svg> built in the HTML namespace parses
// without complaint and draws nothing at all.
func TestTheOwnHoursChartsAreDrawn(t *testing.T) {
	p := open(t)
	p.readyWorker()

	// Two entries on one day and none on the next, so an empty day has something
	// to be empty about.
	p.run("book time", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SendKeys(`#form-timesheet input[name="durationHours"]`, "3.25", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "3.25")

	p.run("open overtime", p.click(`.tab[data-view="overtime"]`))

	// Worth asserting rather than assuming: the click that opens this view landed
	// on a notice instead of the tab until the notices were made transparent to
	// the pointer, and the symptom was a view that simply never opened.
	if !p.visible("#view-overtime") {
		t.Fatal("the overtime view did not open - something is covering the tab")
	}

	if !p.visible("#statistics-card") {
		t.Fatal("the overtime view is open but the statistics card is not visible")
	}

	// An explicit range, so the number of rows below is a fixed expectation. The
	// default is the first of the month to today, which is a different length
	// every day.
	p.run("evaluate",
		chromedp.SetValue("#statistics-from", "2026-08-01", chromedp.ByID),
		chromedp.SetValue("#statistics-to", "2026-08-31", chromedp.ByID),
		p.click("#statistics-load"))

	// A bar exists, and the SVG is in the right namespace - an HTML-namespace
	// <svg> would be found by a selector and occupy no space.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if p.visible("#chart-days svg .chart-bar") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#chart-days svg .chart-bar") {
		t.Fatalf("no bar was drawn for the day chart; the container holds %q",
			truncateText(p.text("#chart-days"), 200))
	}

	var namespace string

	p.run("read the namespace", chromedp.Evaluate(
		`document.querySelector('#chart-days svg')?.namespaceURI ?? ''`, &namespace))

	if namespace != "http://www.w3.org/2000/svg" {
		t.Errorf("the chart is in the %q namespace, so it would render nothing", namespace)
	}

	// The total is on screen, and the figure is the one that was booked.
	if total := p.text("#statistics-total"); !strings.Contains(total, "3.25") {
		t.Errorf("the total says %q, want it to mention 3.25", total)
	}

	// Every day of the month is a row, including the ones with nothing on them:
	// a chart of only the days that have entries reads as a full week.
	var rows int

	p.run("count the rows",
		chromedp.Evaluate(`document.querySelectorAll('#chart-days .chart-track').length`, &rows))

	if rows != 31 {
		t.Errorf("the day chart has %d rows, want 31 - one for every day of August, "+
			"including the empty ones", rows)
	}

	// And the project chart drew the uncategorised bucket, since the entry has no
	// project - which is an answer rather than a gap.
	if !strings.Contains(p.text("#chart-projects"), "3.25") {
		t.Errorf("the project chart does not show the hours: %q",
			truncateText(p.text("#chart-projects"), 200))
	}
}

// A refusal from the server reaches the reader in their own language.
//
// The messages are written where the rule is enforced, in English, which is right
// for the log and wrong for the person who tripped over it: an English sentence was
// shown to a German reader whatever they had chosen.
// The reason now travels as a code with the values the sentence interpolated, and
// the interface looks the sentence up.
//
// Proved in a browser because that is the only place the lookup happens - on the
// wire the message is still English, deliberately, and the integration tests check
// exactly that.
func TestAServerRefusalIsShownInTheReadersLanguage(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// Two accounts in order, because the two halves belong to different jobs. The
	// instance-wide ceiling is configuration and only the built-in administrator may
	// set it; the booking that runs into it is a working day, which that account does
	// not have. Under My account there is a per-account ceiling, and that is not the
	// one this case is about.
	p.run("set a daily ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.SetValue(`#form-operational input[name="maxDailyHours"]`, "8", chromedp.ByQuery),
		p.click(`#form-operational button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.becomeWorker()

	// German after the change of account, not before: the language is a preference of
	// whoever is signed in, so choosing it as the administrator would have set it for
	// the wrong person and left this reader in English.
	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	time.Sleep(300 * time.Millisecond)

	p.run("clear the notices", chromedp.Evaluate(
		`document.querySelector('#toast').replaceChildren()`, nil))

	// A booking over the ceiling: a refusal with four values in it, which is the case
	// that would fall apart if the values were dropped.
	p.run("book over the ceiling",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "9", chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); shown != "" {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if shown == "" {
		t.Fatalf("the refusal raised no notice at all\n\napplication log:\n%s", p.app.Log())
	}

	// The German sentence, not the English one it was built from.
	if !strings.Contains(shown, "Tagesmaximum") {
		t.Errorf("the notice is not the German sentence: %q", shown)
	}

	if strings.Contains(shown, "daily limit") {
		t.Errorf("the notice is still the server's English wording: %q", shown)
	}

	// And the figures survived the translation - 9 booked against a ceiling of 8.
	for _, figure := range []string{"9", "8"} {
		if !strings.Contains(shown, figure) {
			t.Errorf("the notice lost the figure %s: %q", figure, shown)
		}
	}
}

// A field the server rejects is named the way the form names it.
func TestARejectedFieldIsNamedNotIdentified(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	time.Sleep(300 * time.Millisecond)

	// A negative ceiling, which is refused by field rather than by sentence.
	p.run("save an impossible ceiling",
		chromedp.Click(`.tab[data-view="admin"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#form-operational input[name="maxDailyHours"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const field = document.querySelector('#form-operational input[name="maxDailyHours"]');
			field.value = '-3';
			field.removeAttribute('min');
		})()`, nil),
		p.click(`#form-operational button[type="submit"]`),
	)

	shown := ""

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if shown = p.text("#toast"); strings.Contains(shown, "Max/Tag") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !strings.Contains(shown, "Max/Tag") {
		t.Errorf("the refusal does not name the field as the form does: %q\n\n"+
			"application log:\n%s", shown, p.app.Log())
	}

	if strings.Contains(shown, "maxDailyHours") {
		t.Errorf("the refusal still shows the column name: %q", shown)
	}

	if strings.Contains(shown, "invalid parameter") {
		t.Errorf("the refusal still carries GoFr's parameter count: %q", shown)
	}
}

// The enrolment QR code has to be on screen, and has to be the code the server
// drew rather than a broken image.
//
// A picture that fails to load still occupies its box, so "the element is there"
// proves nothing: this checks the browser decoded it, which for an SVG data URI
// means the markup parsed.
func TestTheTwoFactorQRCodeIsShown(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("start two-factor enrolment",
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#totp-card", chromedp.ByID),
		p.click("#totp-begin"),
		chromedp.WaitVisible("#totp-qr", chromedp.ByQuery),
	)

	var loaded bool

	// naturalWidth is 0 for an image the browser could not decode, whatever the
	// element's own size is.
	p.run("check the code decoded", chromedp.Evaluate(`(() => {
		const img = document.querySelector('#totp-qr');
		return Boolean(img && img.complete && img.naturalWidth > 0);
	})()`, &loaded))

	if !loaded {
		t.Errorf("the QR code did not load\n\nsrc: %.60s\n\napplication log:\n%s",
			p.attr("#totp-qr", "src"), p.app.Log())
	}

	if src := p.attr("#totp-qr", "src"); !strings.HasPrefix(src, "data:image/svg+xml") {
		t.Errorf("the code is not an inline SVG: %.60s", src)
	}

	// The typed key is still reachable, folded away behind the picture.
	if p.text("#totp-secret") == "" {
		t.Error("the key to type is gone; a machine with no camera has no way in")
	}

	// Leaving the screen and coming back must not leave the secret on it: the code
	// encodes it, and the enrolment it belonged to is over.
	//
	// Away via Users, because this is the built-in administrator and the time screens
	// are not its. Any other screen would do; the point is only to leave this one.
	p.run("leave and come back",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`.tab[data-view="settings"]`, chromedp.ByQuery),
		chromedp.Sleep(400*time.Millisecond),
	)

	if p.visible("#totp-qr") {
		t.Error("the QR code survived the enrolment it belonged to")
	}

	if secret := p.text("#totp-secret"); secret != "" {
		t.Errorf("the secret is still on screen after leaving: %q", secret)
	}
}

// A first sign-in is walked through the application, without being asked first.
//
// Somebody arriving in an application nobody has introduced is the moment they
// decide it is complicated. The walk used to be offered by a modal with "Show me
// around" and "Not now" on it, and "Not now" recorded the tour as seen - so the
// button that looked like "later" meant "never", and the introduction this
// application has was the introduction almost nobody got.
func TestAFirstSignInIsWalkedThroughTheApplication(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// An ordinary user, because the built-in administrator is deliberately not
	// walked through: it arrives at the setup wizard, and a walk through booking
	// time would be a walk through somebody else's job.
	p.run("create a user",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Rieke", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "rieke@example.com", chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "rieke-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	// The administrator was not, which is half the requirement.
	if p.visible("#tour-bubble") {
		t.Error("the built-in administrator was walked through the application")
	}

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("rieke@example.com", "rieke-password-1")
	p.waitGone("#login-screen")

	// No click anywhere: it starts by itself.
	p.run("wait for the walk through",
		chromedp.WaitVisible("#tour-bubble", chromedp.ByQuery))

	// The walk has to be a walk: the first step counts itself, and Next moves on.
	first := p.text("#tour-title")

	if first == "" {
		t.Error("the first step has no title")
	}

	if count := p.text("#tour-count"); count == "" {
		t.Error("the tour does not say where in it you are")
	}

	// Long enough to be the tour of an application rather than of one screen. Every
	// step outside the screen it started on used to be dropped - the reachability
	// check asked for offsetParent, which is null for everything inside a hidden
	// view - so a walk begun on the time entries was four steps long and looked
	// complete.
	if total := tourTotal(p); total < 12 {
		t.Errorf("the walk is %d steps, which is one screen's worth rather than the "+
			"whole application", total)
	}

	p.run("next step", p.click("#tour-next"))
	time.Sleep(400 * time.Millisecond)

	if second := p.text("#tour-title"); second == first {
		t.Errorf("Next did not move on; still on %q", first)
	}

	p.run("leave the tour", p.click("#tour-end"))
	time.Sleep(400 * time.Millisecond)

	if p.visible("#tour-bubble") {
		t.Error("skipping did not end the tour")
	}

	// Seen once: a reload must not start it again, or leaving it would mean
	// nothing.
	p.run("reload", chromedp.Reload())
	p.waitGone("#login-screen")

	time.Sleep(800 * time.Millisecond)

	if p.visible("#tour-bubble") {
		t.Error("the walk through started again after being left")
	}
}

// The greeting is a screen, reachable from the title, and it says what today has
// on it.
//
// It used to be two half-measures: a modal on a first sign-in, and a card wedged
// above the time entries afterwards. Neither could be gone back to - the only ways
// to read the greeting were to be new or to have just arrived - which is why it is
// a screen now, and why the title in the header leads to it from anywhere.
func TestTheGreetingIsAScreenReachableFromTheTitle(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.run("create a user",
		chromedp.Click(`.tab[data-view="users"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Sven", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "sven@example.com", chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "sven-password-1", chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`),
	)

	time.Sleep(500 * time.Millisecond)

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("sven@example.com", "sven-password-1")
	p.waitGone("#login-screen")

	// The walk through, out of the way: what this case is about is the screen
	// behind it.
	p.settleWelcome()

	// Somewhere else entirely, so that arriving at the greeting is a navigation
	// rather than where the page happened to be.
	p.run("go to the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID))

	p.run("press the title", p.click("#app-title"),
		chromedp.WaitVisible("#view-welcome", chromedp.ByID))

	if title := p.text("#welcome-title"); !strings.Contains(title, "Sven") {
		t.Errorf("the greeting does not name the person: %q", title)
	}

	// It says something about today rather than only hello - what somebody would
	// otherwise have to go and look up.
	if today := p.text("#welcome-today"); today == "" {
		t.Error("the greeting says nothing about today")
	}

	// The points offered are the ones this person can act on, and there is
	// something there at all: the list is built from permissions, so a near-empty
	// greeting means the building went wrong rather than that there is little to
	// say.
	points := p.text("#welcome-points")

	if len(points) < 60 {
		t.Errorf("the greeting lists almost nothing this person can do: %q", points)
	}

	if strings.Contains(strings.ToLower(points), "genehmig") {
		t.Errorf("the greeting promises approvals, which nobody does any more: %q", points)
	}

	// And it is a screen you leave the way you leave any other.
	p.run("carry on", p.click("#welcome-continue"))
	p.waitGone("#view-welcome")
}

// Signing out and back in returns to the screen that was open.
//
// Both sign-in paths used to jump to the first tab the account may see, so the
// screen somebody was working on was discarded and the only way back to it was to
// reload the page afterwards.
func TestSigningInAgainReturnsToTheScreenThatWasOpen(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("go to the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID))

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p.visible("#calendar-days") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if !p.visible("#calendar-days") {
		t.Errorf("signing in again did not return to the calendar\n\napplication log:\n%s",
			p.app.Log())
	}
}

// Somebody else's sign-in does not land on the screen the last person left.
//
// Coming back to where you were is a feature, and it is keyed on the account -
// but it was not the only thing deciding. switchView writes the screen into the
// address bar so a reload returns to it and a link can be sent to somebody, and
// nothing cleared that on the way out. The starting view prefers the address bar
// over the remembered screen, so the next person to sign in on that machine
// arrived on the last one's. Signing in as the same account hid it, because both
// answers agreed.
//
// The rows are the half that matters more. Every loader returns early when the
// right is missing, which is right for loading and wrong for what is already
// loaded - so an ordinary account arriving after an administrator found the
// account list still in the document, under a tab that is only hidden.
func TestAnotherAccountDoesNotInheritTheLastOnesScreen(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	p.createOrdinaryAccount(t, "nachher@example.com", "another-password-1")

	// Somewhere only an administrator can be, so arriving there as somebody else
	// would be unmistakable.
	p.run("go to the accounts", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.waitForText("#table-users tbody", "admin@local")

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	// The address bar let go of it at sign-out, rather than at the next arrival:
	// until then it sits on the sign-in screen naming where somebody was.
	if hash := p.location(); strings.Contains(hash, "#users") {
		t.Errorf("the address bar still names the last screen after signing out: %q", hash)
	}

	// And the rows went with it. This is checked while nobody is signed in at
	// all, which is the strongest form of the question: there is no account for
	// them to belong to.
	if rows := p.count("#table-users tbody tr"); rows != 0 {
		t.Errorf("%d account row(s) are still in the document after signing out", rows)
	}

	p.signIn("nachher@example.com", "another-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	// The greeting, not the accounts. This account has no tab for them.
	if p.visible("#view-users") {
		t.Error("an ordinary account arrived on the administrator's screen")
	}

	if rows := p.count("#table-users tbody tr"); rows != 0 {
		t.Errorf("%d account row(s) from the previous session are on screen for an "+
			"account that may not list accounts", rows)
	}
}

// tourTotal reads how many steps the walk has, out of "Step 1 of 20".
func tourTotal(p *page) int {
	fields := strings.Fields(p.text("#tour-count"))
	if len(fields) == 0 {
		return 0
	}

	total, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return 0
	}

	return total
}

// The spreadsheet card: an export that downloads, and an import that shows what a
// file would do before it does it.
//
// The preview is the part worth driving in a browser. A file assembled by hand is
// wrong more often than it is right, and the whole point is that somebody sees
// which rows are refused and why, on screen, with the import button withheld until
// the file is clean.
func TestTheImportShowsWhatAFileWouldDoBeforeDoingIt(t *testing.T) {
	p := open(t)
	p.readyWorker()

	// A file with one good row and one that no ceiling allows, written through the
	// same writer the export uses.
	book, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "This one is fine"},
		{Date: time.Now().AddDate(0, 0, 1), Hours: 30, Description: "This one is not"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	path := filepath.Join(t.TempDir(), "entries.xlsx")
	if err := os.WriteFile(path, book, 0o600); err != nil {
		t.Fatalf("writing the workbook: %v", err)
	}

	p.run("choose the file",
		chromedp.Click(`.tab[data-view="timesheets"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#workbook-card", chromedp.ByID),
		chromedp.SetUploadFiles("#wb-file", []string{path}, chromedp.ByQuery),
	)

	time.Sleep(300 * time.Millisecond)

	if !p.visible("#wb-preview") {
		t.Fatal("choosing a file did not offer to check it")
	}

	p.run("check the file", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-preview-wrap", chromedp.ByID))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-workbook tbody"), "This one is fine") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	shown := p.text("#table-workbook tbody")

	if !strings.Contains(shown, "This one is fine") {
		t.Fatalf("the preview does not show the file's rows: %q\n\napplication log:\n%s",
			shown, p.app.Log())
	}

	// The refused row says why, on its own line.
	var rejected int

	p.run("count the refused rows", chromedp.Evaluate(
		`document.querySelectorAll('#table-workbook tbody tr.rejected').length`, &rejected))

	if rejected != 1 {
		t.Errorf("%d row(s) are marked as refused, want 1", rejected)
	}

	if summary := p.text("#wb-summary"); !strings.Contains(summary, "1") {
		t.Errorf("the summary does not say how many rows are usable: %q", summary)
	}

	// And the import is withheld: offering it for a file that would be refused is
	// offering a failure.
	if p.visible("#wb-import") {
		t.Error("the import was offered for a file with a refused row in it")
	}

	// Nothing was written by looking.
	if entries := p.text("#table-timesheets tbody"); strings.Contains(entries, "This one is fine") {
		t.Error("the preview created entries")
	}

	// A clean file is offered, and goes through.
	clean, err := spreadsheet.Write([]spreadsheet.Row{
		{Date: time.Now(), Hours: 2, Description: "Imported from a file"},
	})
	if err != nil {
		t.Fatalf("building the second workbook: %v", err)
	}

	cleanPath := filepath.Join(t.TempDir(), "clean.xlsx")
	if err := os.WriteFile(cleanPath, clean, 0o600); err != nil {
		t.Fatalf("writing the second workbook: %v", err)
	}

	p.run("choose a clean file",
		chromedp.SetUploadFiles("#wb-file", []string{cleanPath}, chromedp.ByQuery))

	time.Sleep(300 * time.Millisecond)

	p.run("check it", p.click("#wb-preview"),
		chromedp.WaitVisible("#wb-import", chromedp.ByID))

	p.run("import it", p.click("#wb-import"))

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.text("#table-timesheets tbody"), "Imported from a file") {
			return
		}

		time.Sleep(300 * time.Millisecond)
	}

	t.Fatalf("the imported entry never reached the table\n\ntable:\n%s\n\napplication log:\n%s",
		p.text("#table-timesheets tbody"), p.app.Log())
}

// The loading strip appears while a request is in flight and goes when it is not.
//
// Two failure modes are worth pinning. One is a strip that never appears, which
// makes a slow screen look frozen. The other is a strip that never leaves — an
// in-flight counter that decrements on success only would stick on the first
// failed request and stay for as long as the page is open.
func TestTheLoadingStripAppearsAndGoesAgain(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	// The screen has just been filled, so the page is given until it is actually at
	// rest. Checking "nothing on screen" the instant readyAdmin returns checked it
	// against a page that was still loading, and passed or failed on how fast the
	// last request happened to be - which is how CI came to report a strip that was
	// showing because a request really was in flight.
	p.atRest()

	// Through api(), which is what counts requests. This used raw fetch, at a path
	// that does not exist - so it never touched the counter and the strip could not
	// have appeared for it. The one time it reported "showing" was the leak below
	// being caught, not the strip working.
	var counted struct {
		Peak    int  `json:"peak"`
		Showing bool `json:"showing"`
	}

	p.run("watch a counted request", chromedp.Evaluate(`(async () => {
		// The log endpoint is the administrator's and always answers, which is all
		// this needs. Through api(), so progressStart and progressDone run.
		const inFlight = api('/admin/logs?limit=1');

		// Read at once: whether the strip is drawn depends on the request outliving
		// a deliberate delay, and a fast one correctly shows nothing. What must be
		// true either way is that the request was counted while it was open.
		const peak = progress.inFlight;

		await new Promise((r) => setTimeout(r, 400));

		const bar = document.querySelector('#progress');
		const showing = Boolean(bar) && !bar.hidden;

		await inFlight.catch(() => {});

		return { peak, showing };
	})()`, &counted, awaitPromise))

	if counted.Peak < 1 {
		t.Error("a request through api() was never counted, so the strip has nothing " +
			"to go on")
	}

	// Not asserted: on a fast local server the request can finish inside the delay,
	// which is the strip behaving correctly. That it is drawn at all for a request
	// that does outlive the delay is pinned deterministically by
	// TestTheStripGoesAwayWhenARequestLandsDuringItsFade.
	t.Logf("the strip was drawn during the request: %v", counted.Showing)

	// What must hold either way: nothing outstanding, nothing on screen.
	p.atRest()

	// And a request that fails leaves it clean too - the counter has to come down in
	// a finally, not on the success path. Through api() again, and api() throws for a
	// 404, which is the point.
	p.run("make a counted request that fails", chromedp.Evaluate(`(async () => {
		try { await api('/does-not-exist'); } catch {}
	})()`, nil, awaitPromise))

	p.atRest()
}

// The strip goes away when a request starts during its fade and finishes quickly.
//
// This is the sequence that left it standing:
//
//	a slow request is shown, finishes, and starts fading out;
//	inside that fade another request starts, which cancels it;
//	that one finishes before it was itself worth showing.
//
// Dropping the pending "show it" was only half the job - nothing re-armed the fade.
// Invisible, because the inner span had already faded, but present, and the counter
// out of step with the screen until some later request happened to put the strip up
// again.
//
// Driven through progressStart and progressDone rather than real requests, because
// the race needs the second request to land inside the fade, and a test that waits
// for that by luck is a test that passes for the wrong reason. No sign-in: these two
// functions touch nothing but #progress and their own counter, and the less that is
// running, the less can be in flight when the replay starts.
//
// This also pins the half its sibling only reports on - that a request outliving the
// delay is drawn at all.
func TestTheStripGoesAwayWhenARequestLandsDuringItsFade(t *testing.T) {
	p := open(t)

	// The counter has to start at zero, or progressStart sees a request already in
	// flight and takes an early return - and then the replay drives nothing while the
	// other request's own fade hides the strip, which is a pass for entirely the wrong
	// reason. The pre-fix code passes this test if that is allowed to happen.
	p.atRest()

	// Two independent facts, kept apart. Collapsed into one boolean, "the strip was
	// never drawn" is reported as "the strip never left", and the reader goes looking
	// for the wrong fault.
	var replay struct {
		Shown  bool `json:"shown"`
		Hidden bool `json:"hidden"`
	}

	// The waits are read out of the application rather than written here, so raising
	// either constant cannot turn this into a failure blaming the other one.
	p.run("replay the sequence", chromedp.Evaluate(`(async () => {
		const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

		// Comfortably past the delay, so the strip is put up rather than skipped.
		progressStart();
		await sleep(PROGRESS_DELAY_MS * 2 + 40);

		const shown = !document.querySelector('#progress').hidden;

		// Finishing starts the fade; a new request lands inside it and finishes
		// before it is itself worth showing.
		progressDone();
		progressStart();
		progressDone();

		// Past the fade, so a re-armed one has certainly run.
		await sleep(PROGRESS_FADE_MS + PROGRESS_DELAY_MS * 2 + 40);

		return { shown, hidden: document.querySelector('#progress').hidden };
	})()`, &replay, awaitPromise))

	if !replay.Shown {
		t.Fatal("the loading strip was never drawn for a request that outlived the " +
			"delay, so the rest of this proves nothing")
	}

	if !replay.Hidden {
		t.Error("the loading strip is still on screen after a request that started " +
			"during its fade and finished quickly; the counter is out of step with what " +
			"is drawn, and stays that way until something puts the strip up again")
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

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

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

	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

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

// The evaluation can be read as bars, as columns, or as a circle.
//
// Three shapes were asked for and three shapes were built, and nothing looked at
// them again. The switch is one listener writing a field and calling the drawing
// function, which is exactly the kind of thing that survives a refactor by being
// silently disconnected: the buttons stay, the labels stay, and pressing them
// stops changing the picture. Only a browser notices, because the failure is that
// the same SVG comes back.
func TestTheEvaluationDrawsInWhicheverShapeIsChosen(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("book an hour", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#form-timesheet", chromedp.ByID),
		chromedp.SetValue(`#form-timesheet input[name="durationHours"]`, "2.5",
			chromedp.ByQuery),
		p.click(`#form-timesheet button[type="submit"]`))

	p.waitForText("#table-timesheets tbody", "2.5")

	p.run("evaluate", p.click(`.tab[data-view="report"]`),
		chromedp.WaitVisible("#form-report", chromedp.ByID),
		p.click(`#form-report button[type="submit"]`),
		chromedp.WaitVisible("#report-result", chromedp.ByID))

	// Bars to begin with, which is the remembered default.
	if shape := p.chartShape(); shape != "rect" {
		t.Errorf("the evaluation opens as %q rather than as bars", shape)
	}

	p.run("draw it as a circle", p.click(`#report-chart-switch button[data-chart="pie"]`))

	// A circle is drawn as paths, or as one circle where a single part is the
	// whole. Either says the shape changed; a rect says the press did nothing.
	if shape := p.chartShape(); shape != "path" && shape != "circle" {
		t.Errorf("pressing the circle drew %q", shape)
	}

	p.run("draw it as columns", p.click(`#report-chart-switch button[data-chart="columns"]`))

	if shape := p.chartShape(); shape != "rect" {
		t.Errorf("pressing columns drew %q", shape)
	}

	// And the pressed one says so, for anybody reading the page rather than
	// looking at it.
	p.run("draw it as a circle again", p.click(`#report-chart-switch button[data-chart="pie"]`))

	var pressed string

	p.run("read which button is pressed", chromedp.Evaluate(
		`document.querySelector('#report-chart-switch button[aria-pressed="true"]')
			?.dataset.chart ?? ''`, &pressed))

	if pressed != "pie" {
		t.Errorf("the pressed button is %q while a circle is drawn", pressed)
	}

	// The German word is "Kreis" and not "Kuchen", which is what it used to say.
	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

	p.waitForText("#report-chart-switch", "Kreis")

	if labels := p.text("#report-chart-switch"); strings.Contains(labels, "Kuchen") {
		t.Errorf("the chart switch reads %q", labels)
	}

	// And the shape survived the language change rather than falling back to the
	// default, because the redraw goes through the same state the buttons write.
	if shape := p.chartShape(); shape != "path" && shape != "circle" {
		t.Errorf("switching language redrew the chart as %q, losing the chosen shape",
			shape)
	}
}

// chartShape names the element the evaluation's chart is currently drawn with.
func (p *page) chartShape() string {
	p.t.Helper()

	var shape string

	p.run("read the chart's shape", chromedp.Evaluate(
		`(() => {
			const svg = document.querySelector('#report-chart svg');
			if (!svg) return 'nothing';

			for (const name of ['path', 'circle', 'rect']) {
				if (svg.querySelector(name)) return name;
			}

			return 'unknown';
		})()`, &shape))

	return shape
}

// A reload leaves you on the screen you were reading.
//
// The address bar carries the open screen, so a reload has something to go back
// to and a link to a screen is a link. Without it every reload landed on the
// greeting, which is the wrong answer twice: it loses the place, and it does so
// at the moment somebody pressed F5 because a screen looked stale.
//
// Reachable only through a browser: the state lives in the address bar and is
// applied while the page boots.
func TestAReloadStaysOnTheScreenThatWasOpen(t *testing.T) {
	p := open(t)
	p.readyWorker()

	p.run("open the calendar", p.click(`.tab[data-view="calendar"]`),
		chromedp.WaitVisible("#view-calendar", chromedp.ByID))

	// The address bar names it, which is what survives the reload.
	var hash string

	p.run("read the address", chromedp.Evaluate(`window.location.hash`, &hash))

	if !strings.Contains(hash, "calendar") {
		t.Fatalf("the address bar reads %q after opening the calendar, so a reload "+
			"has nothing to go back to", hash)
	}

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")

	// The calendar again, and not the greeting.
	p.run("wait for the calendar", chromedp.WaitVisible("#view-calendar", chromedp.ByID))

	if p.visible("#view-welcome") {
		t.Error("the reload landed on the greeting rather than on the calendar")
	}

	var open string

	p.run("read which tab is current", chromedp.Evaluate(
		`document.querySelector('.tab[aria-current="true"]')?.dataset.view ?? ''`, &open))

	if open != "calendar" {
		t.Errorf("the navigation marks %q as the open screen after the reload", open)
	}
}

// Clearing the logo puts the shipped mark back at once, not at the next reload.
//
// The reload case was already covered and passed, which is what let this
// through: the document the server sends is correct the moment it is asked for,
// so anything that reloads sees the right icon whatever the page did. What
// somebody actually does is press Save and look at the tab.
func TestClearingTheLogoChangesTheTabWithoutAReload(t *testing.T) {
	p := open(t)
	p.readyAdmin()

	const logo = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSI2MDAiIGhlaWdodD0iMTIwIj48cmVjdCB3aWR0aD0iNjAwIiBoZWlnaHQ9IjEyMCIgZmlsbD0iIzFmNGU3OSIvPjwvc3ZnPg=="

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.storeBranding(t, logo)
	p.run("reload with the logo", chromedp.Reload(),
		chromedp.WaitVisible("#who", chromedp.ByID))

	if !p.iconIsTheLogo(t) {
		t.Fatal("the logo never reached the tab, so this case cannot show it going")
	}

	// Saved through the form's own path rather than by reloading afterwards, which
	// is the whole point: the page has to notice.
	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-branding", chromedp.ByID))

	p.run("take the logo off", p.click("#logo-clear"),
		p.click(`#form-branding button[type="submit"]`))

	p.waitForText("#toast", "")

	// Given a moment for the save and the branding reload behind it, and then
	// asked. No reload anywhere in this case.
	deadline := time.Now().Add(10 * time.Second)

	for p.iconIsTheLogo(t) {
		if time.Now().After(deadline) {
			t.Fatal("the tab still shows the logo after it was removed and saved; " +
				"it only changes on the next reload")
		}

		time.Sleep(200 * time.Millisecond)
	}
}
