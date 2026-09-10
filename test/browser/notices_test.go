//go:build browser

package browser

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

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
	t.Parallel()

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
	t.Parallel()

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

// Nothing an administrator was shown stays on the screen for whoever signs in
// next.
//
// The release notice is drawn for whoever may act on it, which is a permission
// an ordinary account has not got - and checkForRelease used to return without
// taking it down when the server refused. Sign out of an administrator and in as
// somebody who only works here, in the same page, and the previous account's
// news was still there: the version this installation runs, and a button into
// the screen that administers it.
//
// Reported by somebody who signed in as a synchronised directory user and found
// both.
func TestTheReleaseNoticeDoesNotOutlastTheAccountItWasFor(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Drawn by hand rather than waited for. Whether a newer release exists
	// depends on somebody else's feed, and what this is about is what happens to
	// the banner afterwards - not how it got there.
	p.run("put the notice up", chromedp.Evaluate(`
		(() => {
			const banner = document.querySelector('#release-banner');
			document.querySelector('#release-text').textContent =
				'Version v9.9.9 is available.';
			banner.hidden = false;
			return 1;
		})()`, nil))

	if !p.visible("#release-banner") {
		t.Fatal("the fixture did not put the notice up, so this proves nothing")
	}

	p.becomeWorker()

	if p.visible("#release-banner") {
		t.Errorf("the release notice survived the sign-out; it says %q",
			p.text("#release-text"))
	}

	// And again, without a sign-out in between - which is what actually proves
	// the other half. The first version of this case only exercised the logout
	// path and passed with the refusal path still broken.
	//
	// The notice is put back up and the check is asked to run: this account is
	// answered 403, and the answer has to take it down rather than leave it.
	p.run("put it back up", chromedp.Evaluate(`
		(() => {
			document.querySelector('#release-text').textContent = 'Version v9.9.9 is available.';
			document.querySelector('#release-banner').hidden = false;
			return 1;
		})()`, nil))

	p.run("ask again as this account",
		chromedp.Evaluate(`checkForRelease()`, nil, awaitPromise))

	if p.visible("#release-banner") {
		t.Errorf("the check was refused for this account and left the notice "+
			"standing; it says %q", p.text("#release-text"))
	}
}

// A restart that is waiting is said on every screen, to the people who can act
// on it, and stops being said when it stops being true.
//
// It was a card under Settings. A card is read by somebody who has already gone
// looking, and the thing it reports is not about that screen - it is about the
// installation: saved values that the running process has not got. An
// administrator who saved something, went off to do anything else and came back
// the next day had nothing anywhere telling them so.
//
// Three things, because each of them is a way the old card was wrong or a way a
// banner could be: it has to appear away from the screen that caused it, it has
// to go when the settings go back, and it must not be shown to somebody who
// could do nothing about it.
func TestARestartThatIsWaitingIsSaidOnEveryScreenAndOnlyToAdministrators(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	if p.visible("#restart-banner") {
		t.Fatal("a freshly started instance says a restart is waiting")
	}

	// Saved the way the screen saves it, so this is the same path an
	// administrator takes rather than a request shaped to suit the case.
	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))
	p.settled()

	p.run("point tracing somewhere",
		chromedp.SetValue(`#form-telemetry [name="traceExporter"]`, "otlp", chromedp.ByQuery),
		chromedp.SetValue(`#form-telemetry [name="tracerUrl"]`, "jaeger:4317", chromedp.ByQuery),
		p.click(`#form-telemetry button[type="submit"]`))

	p.waitShown("#restart-banner")

	// Away from the screen that caused it, which is the whole reason it is a
	// banner: the tab below is somewhere else entirely.
	//
	// Accounts rather than time entries. This is the built-in administrator,
	// which administers and records nothing, so there is no timesheet tab for it
	// to be sent to.
	p.run("go somewhere else", p.click(`.tab[data-view="users"]`))
	p.waitShown("#view-users")

	if !p.visible("#restart-banner") {
		t.Error("the restart notice is only on the screen that caused it, which is " +
			"the screen somebody has already left by the time it matters")
	}

	// And it says that something is waiting, with the way to what.
	//
	// It used to list them here. The list is on the card now: a card's worth of
	// detail stuck to the top of every screen was saying what the card under
	// Settings says, and the notice's job is to be seen from anywhere and lead
	// somewhere.
	if said := p.text("#restart-summary"); said == "" {
		t.Error("the notice does not say anything is waiting")
	}

	p.run("follow the notice", p.click("#restart-open"))
	p.waitShown("#restart-card")

	if said := p.text("#restart-card-pending"); !strings.Contains(said, "jaeger:4317") {
		t.Errorf("the card does not say what is waiting: %q", said)
	}

	// Put back, and it goes with them. Reset rather than clearing the fields one
	// at a time: it is the control that means "as it was", and the notice has to
	// follow it or the installation is left claiming a restart it does not need.
	p.run("back to Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))
	p.run("reset", p.click("#telemetry-reset"))

	p.waitGone("#restart-banner")

	// And somebody who cannot administer this installation is not told about it.
	p.run("save it again",
		chromedp.SetValue(`#form-telemetry [name="traceExporter"]`, "otlp", chromedp.ByQuery),
		chromedp.SetValue(`#form-telemetry [name="tracerUrl"]`, "jaeger:4317", chromedp.ByQuery),
		p.click(`#form-telemetry button[type="submit"]`))

	p.waitShown("#restart-banner")
	p.becomeWorker()

	if p.visible("#restart-banner") {
		t.Error("somebody who may not administer this installation is told it is " +
			"waiting for a restart they cannot perform")
	}
}

// The release notice cannot be clicked away, and stands where the restart one
// does.
//
// It was dismissable, and the dismissal was remembered against that version.
// That reads as a courtesy and works out as an installation three releases
// behind, arrived at by clicking a cross once on a busy morning - which is the
// exact failure the banner was added to prevent, since the version card had
// been saying the same thing on a screen nobody visits.
//
// It reports a state rather than news: a newer version exists whether or not
// anybody wants to hear it, and it goes when it stops being true.
func TestTheReleaseNoticeCannotBeClickedAway(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	// Before anything is said about the banner: the watch the sign-in started
	// is answering behind this case, and its answer takes the banner down.
	p.settleReleaseWatch()

	// Put up the way the watch puts it up, because no newer version exists to
	// wait for here.
	p.run("a newer version exists", chromedp.Evaluate(`
		(() => {
			drawReleaseBanner({ latest: 'v9.9.9', running: 'v0.0.1' });

			return 1;
		})()`, nil))

	p.waitShown("#release-banner")

	var shape string

	p.run("look at it", chromedp.Evaluate(`
		(() => {
			const banner = document.querySelector('#release-banner');

			// Read defensively: this runs to describe a page that may not have
			// the box at all, and an exception here would say only that
			// something threw.
			const standing = document.querySelector('#standing-notices');

			return JSON.stringify({
				dismiss: banner.querySelectorAll('.banner-dismiss, #release-dismiss').length,
				standing: Boolean(banner.closest('#standing-notices')),
				withRestart: Boolean(document.querySelector('#standing-notices #restart-banner')),
				sticky: standing ? getComputedStyle(standing).position : 'no such box',
			});
		})()`, &shape))

	if !strings.Contains(shape, `"dismiss":0`) {
		t.Errorf("the release notice still offers a way to click it away: %s", shape)
	}

	if !strings.Contains(shape, `"standing":true`) || !strings.Contains(shape, `"withRestart":true`) {
		t.Errorf("the release notice does not stand where the restart notice does: %s", shape)
	}

	if !strings.Contains(shape, `"sticky":"sticky"`) {
		t.Errorf("the standing notices scroll away with the page: %s", shape)
	}

	// And it is still only for the people who can act on it.
	p.becomeWorker()

	if p.visible("#release-banner") {
		t.Error("somebody who may not administer this installation is told a newer " +
			"version exists")
	}
}
