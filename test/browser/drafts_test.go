//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// What was typed and not saved is still there after the page is loaded again.
//
// Reloading is not always a decision. It is a stray F5, a browser deciding the
// tab has been idle long enough, a certificate prompt, a laptop coming back from
// sleep - and whatever the reason, everything in a half-filled form was gone,
// with the form coming back from the server looking like one nobody had touched.
// There was not even anything on screen afterwards to say it had happened.
//
// The password is the exception and is checked here rather than left implied: a
// draft is written to storage, and nothing is worth putting a password there
// for.
func TestWhatWasTypedIsStillThereAfterAReload(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	// A server dialect, or the fields typed into below are not on screen at all.
	p.run("choose postgres",
		chromedp.SetValue(`#form-datasource select[name="dialect"]`, "postgres",
			chromedp.ByQuery))

	typed := map[string]string{
		"host": "db.example.invalid", "name": "gtr_live", "user": "gtr_admin",
	}

	for field, value := range typed {
		p.run("type the "+field, chromedp.SendKeys(
			`#form-datasource [name="`+field+`"]`, value, chromedp.ByQuery))
	}

	const secret = "a-password-that-must-not-be-written-down"

	p.run("type the password", chromedp.SendKeys(
		`#form-datasource [name="password"]`, secret, chromedp.ByQuery))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settleWizard()
	p.settled()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	if got := p.value(`#form-datasource select[name="dialect"]`); got != "postgres" {
		t.Errorf("the type reads %q after a reload; postgres was chosen", got)
	}

	for field, value := range typed {
		if got := p.value(`#form-datasource [name="` + field + `"]`); got != value {
			t.Errorf("the %s field reads %q after a reload; %q was typed into it",
				field, got, value)
		}
	}

	if got := p.value(`#form-datasource [name="password"]`); got != "" {
		t.Errorf("the password came back after a reload as %q", got)
	}

	// And it is not sitting in storage waiting to.
	var kept string

	p.run("read what was written down", chromedp.Evaluate(`
		JSON.stringify(Object.keys(sessionStorage)
			.filter((key) => key.startsWith('gtr.draft.'))
			.map((key) => sessionStorage.getItem(key)))`, &kept))

	if strings.Contains(kept, secret) {
		t.Error("the password was written into storage along with the rest of the form")
	}

	// And signing out takes the lot: a draft belongs to whoever typed it, and
	// this application does not leave one person's half-written work for the next.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var after string

	p.run("read them again", chromedp.Evaluate(`
		JSON.stringify(Object.keys(sessionStorage).filter((key) => key.startsWith('gtr.draft.')))`,
		&after))

	if after != "[]" {
		t.Errorf("drafts survived signing out: %s", after)
	}
}

// A draft written by an older version is dropped rather than put back.
//
// A draft is a snapshot of a form, and a form is not a fixed thing: fields are
// added, a default changes, a card starts filling something in that it used to
// leave blank. A draft from before such a change is no longer a record of what
// somebody typed - it is a record of how the screen used to behave.
//
// Reported as exactly that. The connection card learnt to name the database this
// process is connected to; a draft from the version before still held the empty
// type it used to leave, and restoring it put the card back to naming nothing -
// on an installation that had just been updated to fix precisely that.
func TestADraftFromAnotherVersionIsNotPutBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")
	p.settled()

	running := p.value(`#form-datasource select[name="dialect"]`)

	if running == "" {
		t.Fatal("the card names no type before anything was drafted, so this case " +
			"is about to prove nothing")
	}

	// A draft of the shape the version before this one left behind: the type
	// blank, written down under a version that is not the one running.
	p.run("plant a draft from an older version", chromedp.Evaluate(`
		(() => {
			// Under this account's own prefix: a draft belongs to whoever wrote
			// it, and one filed under anybody else is not found at all - which
			// would make this case pass without proving anything.
			sessionStorage.setItem('gtr.draft.' + me.user.id + '.form-datasource',
				JSON.stringify({
				version: 'v0.0.1-before',
				values: { dialect: '', name: '', host: '', port: '', user: '', sslMode: '' },
			}));

			return 1;
		})()`, nil))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settleWizard()
	p.settled()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	if got := p.value(`#form-datasource select[name="dialect"]`); got != running {
		t.Errorf("the card names %q after a draft from another version was found; "+
			"it named %q before, and the process has not changed database", got, running)
	}

	// And it is gone rather than skipped again on every load.
	var left string

	p.run("look for the draft", chromedp.Evaluate(
		`String(sessionStorage.getItem('gtr.draft.' + me.user.id + '.form-datasource') ?? '')`,
		&left))

	if left != "" {
		t.Errorf("the draft from another version is still stored: %s", left)
	}

	// What this version writes is stamped, or the next update repeats all of it.
	p.run("type something", chromedp.SendKeys(
		`#form-datasource [name="name"]`, "gtr_live", chromedp.ByQuery))

	var stamped string

	p.run("read what was written down", chromedp.Evaluate(
		`String(sessionStorage.getItem('gtr.draft.' + me.user.id + '.form-datasource') ?? '')`,
		&stamped))

	if !strings.Contains(stamped, `"version":"`) || strings.Contains(stamped, `"version":""`) {
		t.Errorf("a draft written now carries no version: %s", stamped)
	}
}

// What was typed beside the clock is still there after a reload.
//
// Almost everything somebody fills in here is in a form, and a form is what a
// draft is filed under. The timer is not: its project and its description sit
// beside the clock rather than in one, because starting a timer is a button
// rather than a submission - so until it is started, the box is the only copy of
// what somebody has typed, and a reload took it.
//
// Once the timer runs the server holds both and gives them back on its own,
// which is why this is about the window before it is started.
func TestWhatWasTypedBesideTheClockSurvivesAReload(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Somebody who records time: the built-in administrator has no timer.
	p.becomeWorker()

	p.run("open the time view", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-description", chromedp.ByID))
	p.settled()

	const doing = "Rechnungslauf vorbereitet"

	p.run("say what is being worked on", chromedp.SendKeys(
		"#timer-description", doing, chromedp.ByID))

	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settled()

	p.run("open the time view again", p.click(`.tab[data-view="timesheets"]`),
		chromedp.WaitVisible("#timer-description", chromedp.ByID))

	if got := p.value("#timer-description"); got != doing {
		t.Errorf("the description beside the clock reads %q after a reload; %q was typed",
			got, doing)
	}
}
