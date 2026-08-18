//go:build browser

package browser

import (
	"strings"
	"testing"

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

	// 1 is OPEN. 0 is still connecting, which on a local instance means something
	// is wrong rather than something is slow, and 2 is closed.
	p.run("read the stream's state", chromedp.Evaluate(
		`announcements ? announcements.readyState : -1`, &state))

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
	p.run("switch to German",
		chromedp.SetValue("#language-picker", "de", chromedp.ByID),
		chromedp.Evaluate(
			`document.querySelector('#language-picker').dispatchEvent(new Event('change'))`, nil))

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
