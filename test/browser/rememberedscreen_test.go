//go:build browser

package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Signing out and back in returns to the screen that was open.
//
// Both sign-in paths used to jump to the first tab the account may see, so the
// screen somebody was working on was discarded and the only way back to it was to
// reload the page afterwards.
func TestSigningInAgainReturnsToTheScreenThatWasOpen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	p.run("go to the calendar",
		chromedp.Click(`.tab[data-view="calendar"]`, chromedp.ByQuery),
		chromedp.WaitVisible("#calendar-days", chromedp.ByID))

	p.run("sign out", p.click("#logout"), chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")

	deadline := time.Now().Add(waitPatience)
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
	t.Parallel()

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
	t.Parallel()

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

// What somebody changes about the screen is theirs, and stays theirs.
//
// Two settings, one rule, seen from four sides: kept on the account, applied
// when that account signs in, gone from the screen when it signs out, and never
// inherited by whoever signs in next.
//
// The appearance was the one that broke it. It lived in the browser, which is
// right for one person with one laptop and wrong everywhere else - the next
// person at a shared machine arrived to the last one's dark mode, on a screen
// with nothing else of theirs on it.
func TestTheScreenSomebodyChoseIsTheirs(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createOrdinaryAccount(t, workerEmail, workerPassword)

	p.run("choose dark", p.chooseOption("#theme-picker", "dark"))
	p.chooseLanguage("de")
	p.waitForText(`.tab[data-view="settings"]`, "Mein Konto")

	// On the account, which is what makes it follow the person rather than the
	// machine. Read back from the server rather than from the page.
	var stored struct {
		Theme    string `json:"theme"`
		Language string `json:"language"`
	}

	read := func() {
		var raw string

		p.run("read the account", chromedp.Evaluate(`
			(async () => {
				const r = await fetch('/api/v1/me', { credentials: 'same-origin', cache: 'no-store' });
				const body = await r.json();

				return JSON.stringify(body?.data?.user ?? {});
			})()`, &raw, awaitPromise))

		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			t.Fatalf("reading the account: %v; %s", err, raw)
		}
	}

	read()

	if stored.Theme != "dark" {
		t.Errorf("the account reads in %q after dark was chosen", stored.Theme)
	}

	if stored.Language != "de" {
		t.Errorf("the account speaks %q after German was chosen", stored.Language)
	}

	screen := func() struct {
		Theme    string `json:"theme"`
		Chosen   string `json:"chosen"`
		Language string `json:"language"`
		Themes   string `json:"themePicker"`
		Speaks   string `json:"languagePicker"`
	} {
		var out struct {
			Theme    string `json:"theme"`
			Chosen   string `json:"chosen"`
			Language string `json:"language"`
			Themes   string `json:"themePicker"`
			Speaks   string `json:"languagePicker"`
		}

		p.evalJSON(`JSON.stringify({
			theme: document.documentElement.dataset.theme,
			chosen: document.documentElement.dataset.themePreference,
			language: document.documentElement.lang,
			themePicker: document.querySelector('#theme-picker').value,
			languagePicker: document.querySelector('#language-picker').value,
		})`, &out)

		return out
	}

	if now := screen(); now.Theme != "dark" || now.Language != "de" {
		t.Fatalf("the screen is %+v after choosing dark and German", now)
	}

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	out := screen()

	if out.Chosen != "auto" {
		t.Errorf("the sign-in screen follows %q rather than the time of day", out.Chosen)
	}

	if out.Language == "de" {
		t.Error("the sign-in screen still speaks the language of the account that left")
	}

	// The controls say what the screen is doing, rather than still offering the
	// choices of somebody who has gone.
	if out.Themes != "auto" || out.Speaks != out.Language {
		t.Errorf("the pickers offer %q and %q on a screen doing %q and %q",
			out.Themes, out.Speaks, out.Chosen, out.Language)
	}

	// Somebody else, on the same machine, who has chosen nothing.
	p.signIn(workerEmail, workerPassword)
	p.waitGone("#login-screen")
	p.settleWelcome()

	next := screen()

	if next.Chosen != "auto" {
		t.Errorf("the next person inherited %q", next.Chosen)
	}

	if next.Language == "de" {
		t.Error("the next person inherited the last one's language")
	}

	// And the first person's choice is still theirs when they come back, which is
	// the difference between clearing it and keeping it per account.
	p.run("sign out again", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn(harness.AdminEmail, adminPassword)
	p.waitGone("#login-screen")
	p.settleWelcome()

	back := screen()

	if back.Chosen != "dark" {
		t.Errorf("coming back, the screen follows %q rather than the dark that was "+
			"chosen", back.Chosen)
	}

	if back.Language != "de" {
		t.Errorf("coming back, the screen speaks %q", back.Language)
	}
}
