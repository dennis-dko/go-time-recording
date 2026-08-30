//go:build browser

package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The page holds a connection open for what the server has to say.
//
// Everything else this interface knows, it asked for. This is the one thing it
// cannot ask for in time: an update replaces the binary underneath and, where the
// platform allows it, restarts into it seconds later. A poll fast enough to warn
// anybody would be a request per second per tab, for ever, to be told nothing.
//
// So the wiring itself is the thing worth checking. It is one EventSource opened
// after sign-in and closed on the way out, and if it is quietly never opened,
// nothing fails - every screen works, and one day an update takes the application
// away from somebody mid-sentence with no warning at all.
func TestThePageListensForWhatTheServerAnnounces(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	var state int

	// 1 is OPEN. 0 is still connecting and 2 is closed.
	//
	// Waited for rather than read once. Connecting is a state a stream passes
	// through, and this used to be read the instant the page was ready: on a run
	// with twenty instances on the machine the handshake had not finished, and a
	// stream that was open a moment later was reported as one that never opened.
	// What is worth asserting is that it gets there, and that it does not sit in
	// connecting for ever - which is what running out of time here still says.
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		p.run("read the stream's state", chromedp.Evaluate(
			`announcements ? announcements.readyState : -1`, &state))

		if state == 1 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if state != 1 {
		t.Fatalf("the announcement stream is in state %d rather than open; "+
			"nothing would reach this page before a restart", state)
	}

	// Signing out closes it. An EventSource left running reopens itself for ever
	// against a session that no longer exists.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	var after int

	p.run("read it again", chromedp.Evaluate(
		`announcements ? announcements.readyState : -1`, &after))

	if after != -1 && after != 2 {
		t.Errorf("the stream is still in state %d after signing out", after)
	}
}

// What arrives is put on screen, in the reader's language, and stays there.
//
// The rendering is called with an announcement rather than waited for, because
// producing a real one means installing a real release. It is the same call the
// stream's own handler makes, with the same words and the same formatting - what
// is being checked is that an announcement becomes something a person sees, and
// that is decided here rather than in transit.
func TestAnAnnouncementBecomesABannerNobodyCanMiss(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	if p.visible("#update-banner") {
		t.Fatal("the banner is up before anything has been announced")
	}

	p.run("an update starts", chromedp.Evaluate(
		`applyAnnouncement({ kind: 'update.installing', version: 'v9.9.9' })`, nil))

	if !p.visible("#update-banner") {
		t.Fatal("nothing appeared when an update was announced")
	}

	banner := p.text("#update-banner")

	if !strings.Contains(banner, "v9.9.9") {
		t.Errorf("the banner does not say which version: %q", banner)
	}

	// The half that matters most to somebody in the middle of typing: this phase
	// does not interrupt them, and the banner says so.
	if !strings.Contains(strings.ToLower(banner), "carry on") {
		t.Errorf("the banner does not say that work can continue: %q", banner)
	}

	// German, because the person who most needs to understand this is the one
	// least likely to be reading the application's source language.
	p.chooseLanguage("de")

	// Waited for rather than assumed: the switch stores the choice, reloads the
	// account and only then redraws, and a case that announces before that
	// finishes gets the old language and reads as a translation fault.
	//
	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	// The banner that was already up changed language with the page. It used to
	// go blank instead, which is the worst of the three possible outcomes: this is
	// the one message on screen that somebody may be reading precisely because
	// they do not understand what is happening to the application.
	if standing := p.text("#update-banner"); !strings.Contains(standing, "installiert") {
		t.Errorf("the standing announcement reads %q after switching to German",
			standing)
	}

	p.run("the restart begins", chromedp.Evaluate(
		`applyAnnouncement({ kind: 'update.restarting', version: 'v9.9.9' })`, nil))

	german := p.text("#update-banner")

	if !strings.Contains(german, "startet") {
		t.Errorf("the restart notice is not in German: %q", german)
	}

	// No way to dismiss it. What it describes does not stop being true because
	// somebody closed it, and a banner that can be closed is one that will be.
	if p.count("#update-banner button") != 0 {
		t.Error("the announcement can be dismissed, which it must not be")
	}
}

// A change of rights arrives on the same connection, and stays on the screen.
//
// The server writes it down the stream the moment the answer moves, which is the
// only route that reaches a page nobody is touching - and that is the case worth
// covering, because the person a right was taken from is by definition not the
// person clicking around to find out.
//
// So two things are checked here, and they are the two that used to be wrong. The
// stream is listened to at all: a listener quietly never registered fails
// nothing, and the notice simply goes back to arriving on the next click. And the
// notice is a banner rather than the toast it was - somebody who stepped away for
// a minute came back to a screen that looked right, was not, and had already
// cleared away the only thing that said so.
func TestAChangeOfRightsArrivesOnTheStreamAndStays(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	if p.visible("#rights-banner") {
		t.Fatal("the notice is up before anything has changed")
	}

	// Delivered the way the server delivers it, through the listener on the stream
	// rather than by calling what the listener calls. That is the half that was
	// missing: the connection was open and nothing was reading this off it.
	p.run("the server says the rights moved", chromedp.Evaluate(`
		announcements.dispatchEvent(new MessageEvent('permissions', {
			data: JSON.stringify({ revision: 'something-else-entirely' }),
		}))`, nil))

	p.waitShown("#rights-banner")

	if standing := p.text("#rights-banner"); !strings.Contains(standing, "may do") {
		t.Errorf("the notice reads %q rather than saying what changed", standing)
	}

	// And not as a toast as well. The stack is where a fading notice would be, and
	// the whole point of this one is that it does not fade.
	if notes := p.text("#toast"); strings.Contains(notes, "may do") {
		t.Errorf("the change was also reported in the corner, where it clears "+
			"itself while nobody is looking: %q", notes)
	}

	// The way out, and the only thing there is to do about it: what a role opens is
	// which screens were built at all, so the page has to be loaded again.
	if p.count("#rights-reload") != 1 {
		t.Error("the notice offers no way to act on it")
	}

	// German, because whoever is being told their screen has stopped being true is
	// owed that in their own language.
	p.chooseLanguage("de")
	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	if standing := p.text("#rights-banner"); !strings.Contains(standing, "Berechtigungen") {
		t.Errorf("the standing notice reads %q after switching to German", standing)
	}

	// It belongs to the account it is about. Left standing, the next person at the
	// same desk is told their rights changed, which they did not.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	if p.visible("#rights-banner") {
		t.Error("the notice outlasted the session it was raised in")
	}
}
