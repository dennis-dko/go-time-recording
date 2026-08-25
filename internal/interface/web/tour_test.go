package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The guided tour points at real controls by selector. That is what keeps it
// honest - it cannot describe a button that no longer exists - but it also
// means a renamed id turns a step into a highlight around nothing. The code
// skips a missing target at run time, so the failure is silent: the step just
// never appears, and nobody notices the tour got shorter.
//
// These tests make that loud instead.

var (
	tourStepPattern   = regexp.MustCompile(`(?s)const TOUR_STEPS = \[(.*?)\n\];`)
	tourTargetPattern = regexp.MustCompile(`target:\s*'([^']+)'`)
	tourViewPattern   = regexp.MustCompile(`view:\s*'([^']+)'`)
)

// tourSection returns the step table as written.
func tourSection(t *testing.T) string {
	t.Helper()

	js := asset(t, "/app.js")

	match := tourStepPattern.FindStringSubmatch(js)
	if match == nil {
		t.Fatal("could not find TOUR_STEPS in app.js")
	}

	return match[1]
}

// Every step has to point at something the page actually contains.
func TestEveryTourStepTargetsSomethingThatExists(t *testing.T) {
	html := asset(t, "/")
	section := tourSection(t)

	targets := tourTargetPattern.FindAllStringSubmatch(section, -1)
	if len(targets) == 0 {
		t.Fatal("the tour has no steps")
	}

	for _, match := range targets {
		selector := match[1]

		// Only id selectors are used, which keeps this check exact rather than
		// an approximation of what a browser would resolve.
		if !strings.HasPrefix(selector, "#") {
			t.Errorf("tour step targets %q; use an id so the target can be verified", selector)

			continue
		}

		if !strings.Contains(html, `id="`+strings.TrimPrefix(selector, "#")+`"`) {
			t.Errorf("tour step points at %q, which is not in the page", selector)
		}
	}
}

// A step that switches to a view has to name one that exists, or the tour
// lands on a blank page.
func TestEveryTourViewExists(t *testing.T) {
	html := asset(t, "/")
	section := tourSection(t)

	for _, match := range tourViewPattern.FindAllStringSubmatch(section, -1) {
		view := match[1]

		if !strings.Contains(html, `id="view-`+view+`"`) {
			t.Errorf("tour step switches to view %q, which is not in the page", view)
		}
	}
}

// The tour is offered on a first sign-in, so it has to be reachable again
// afterwards - otherwise the only way to see it twice is a database edit.
func TestTheTourCanBeRestarted(t *testing.T) {
	html := asset(t, "/")
	js := asset(t, "/app.js")

	if !strings.Contains(html, `id="tour-restart"`) {
		t.Error("expected a control to start the tour again")
	}

	if !strings.Contains(js, "#tour-restart") {
		t.Error("the restart control is not wired to anything")
	}
}

// Every way out has to record the tour as seen, or someone who skipped it is
// greeted with it again on the next sign-in - overriding a decision they
// already made.
func TestSkippingTheTourCountsAsSeen(t *testing.T) {
	js := asset(t, "/app.js")

	end := strings.Index(js, "async function endTour()")
	if end < 0 {
		t.Fatal("could not find endTour in app.js")
	}

	body := js[end : end+900]
	if !strings.Contains(body, "/me/tour") {
		t.Error("ending the tour must record it as seen")
	}

	// The skip button and the finish button both have to go through it.
	if !strings.Contains(js, "$('#tour-end').addEventListener('click', endTour)") {
		t.Error("the skip button must end the tour the same way finishing does")
	}
}

// Two things walking someone through the application at once is worse than
// either alone, and the setup wizard is the one that has to happen first.
//
// The built-in administrator is left out of it as well. That account arrives at the
// wizard, which is its own introduction and covers the screens it actually uses; a
// walk through booking time and reading an overtime balance would be a walk through
// somebody else's job. The card under My account still starts it on request.
func TestTheGreetingYieldsToTheWizardAndSkipsTheAdministrator(t *testing.T) {
	js := asset(t, "/app.js")

	start := strings.Index(js, "async function maybeWelcome()")
	if start < 0 {
		t.Fatal("could not find maybeWelcome in app.js")
	}

	body := js[start : start+600]

	for _, guard := range []struct{ needle, why string }{
		{"#setup-wizard", "the greeting must not appear while the setup wizard is showing"},
		{"tourSeen", "the greeting must only be offered to someone who has not seen it"},
		{"administersOnly()", "an account that only administers must not be greeted with " +
			"a tour of booking time, which is a tour of somebody else's job"},
		{"mustChangePassword", "the greeting must wait until the initial password is replaced"},
	} {
		if !strings.Contains(body, guard.needle) {
			t.Error(guard.why)
		}
	}

	// Declining has to count, or the greeting returns on the next sign-in and
	// overrides a decision somebody already made.
	if !strings.Contains(js, "recordTourSeen()") {
		t.Error("declining the greeting must record it as seen")
	}
}

// The restart notice appears when something is waiting for a restart, and not
// otherwise.
//
// It used to stay on screen wherever restarting is impossible - on Windows,
// which has no execve - whether anything was pending or not, so that nobody
// could discover the limitation by pressing a button that never appears. The
// reasoning was sound and the result was not: a warning that is on the screen
// every time you open it is furniture, read once and looked past thereafter,
// including on the day it finally has something to say.
//
// The limitation is still explained, at the moment it costs something. The first
// save that needs a restart brings the notice up, and it puts the reason where
// the button would have been. Nothing is hidden; it is said when it matters
// instead of always.
//
// Checked in the asset rather than in a browser because the browser suite runs on
// Linux, where restarting is supported and this branch is never taken.
func TestTheRestartNoticeAppearsOnlyWhenSomethingIsWaiting(t *testing.T) {
	js := asset(t, "/app.js")

	if !strings.Contains(js, "banner.hidden = pending.length === 0;") {
		t.Error("the restart notice is shown on an empty pending list, which makes a " +
			"standing warning out of something that should speak up when it has something")
	}

	// And the explanation is translated rather than shown as the server wrote it,
	// keyed on which refusal it is - see TestEveryRestartRefusalIsExplained.
	if !strings.Contains(js, "restart.unsupported.${state.reasonCode") {
		t.Error("the limitation is shown in the server's English wording, or is not " +
			"keyed on which refusal it is")
	}

	// The hint above the list promises saved changes, so it goes when there are
	// none: the card is always on the settings screen, and a standing promise
	// above an empty list reads as a rendering fault.
	if !strings.Contains(js, "$('#restart-card-hint').hidden = !waiting") {
		t.Error("the notice promises a list of pending changes even when there are none")
	}
}

// The restart notice is a banner, and only for the people who administer.
//
// It was a card under Settings, which is only read by somebody who has already
// gone looking - so an administrator who saved something, went off to do
// anything else and came back the next day had nothing anywhere telling them the
// installation was still running on the old values. A banner is on every screen,
// which is where a fact about the whole installation belongs.
//
// That is also what makes the permission its own decision. On a card behind a
// tab that needs settings:manage, who could see it was decided by the tab; a
// banner has no tab to inherit from, and nobody who cannot act on this should be
// told an installation they use is waiting for something.
//
// There is a card again, and this asserted for a while that there was not. The
// reason given was that the same thing would be said twice and one copy would go
// stale - which was right about the risk and wrong about what the card is for.
// The banner is the notice: something is waiting, here is what. The card is the
// control: this installation can be restarted, here is what that does here, and
// here is the button. Without it there was no way to restart on purpose at all,
// and on a container deployment - where what the button does is the interesting
// part - the screen said nothing about it.
//
// The staleness is answered directly instead, below: one function fills both
// from one answer, so there is nothing to drift.
func TestTheRestartNoticeIsABannerAndTheControlIsACard(t *testing.T) {
	js := asset(t, "/app.js")
	html := asset(t, "/")

	if !strings.Contains(html, `id="restart-banner"`) {
		t.Error("there is no restart banner in the markup")
	}

	if !strings.Contains(html, `id="restart-card"`) {
		t.Error("there is no restart card under Settings, so an installation with " +
			"nothing pending offers no way to restart on purpose")
	}

	// The list of what is waiting appears once, and it is on the card: that is
	// where somebody acts on it, and a card's worth of detail stuck to the top of
	// every screen is what the banner used to be.
	if !strings.Contains(html, `id="restart-card-pending"`) {
		t.Error("the card does not list what is waiting, so the banner sends people " +
			"to a card that does not answer the question")
	}

	if strings.Contains(html, `id="restart-pending"`) {
		t.Error("the banner still lists the pending changes as well as the card")
	}

	// The banner points rather than acts, and points with a word in its own
	// sentence rather than a control beside it - what somebody wants to press is
	// the thing being talked about, and a button reading "go to the restart"
	// next to a sentence about a restart is the same word twice.
	//
	// Drawn rather than written into the markup, because it sits inside a
	// sentence whose shape belongs to whoever translated it.
	if !strings.Contains(js, `id: 'restart-open'`) {
		t.Error("the banner offers no way to the card it is a notice about")
	}

	if !strings.Contains(js, "').split('{1}')") {
		t.Error("the link is put beside the sentence rather than into it, so a " +
			"translation cannot decide where in the sentence it falls")
	}

	if strings.Contains(html, `id="restart-now"`) {
		t.Error("the banner still carries a restart button of its own")
	}

	// Not dismissable, and nothing to dismiss it with. It is a state rather than
	// news: it goes when the settings are put back or the application restarts.
	if strings.Contains(html, "restart-dismiss") {
		t.Error("the restart banner can be dismissed; it reports a state, and a " +
			"state that has been clicked away is still the state")
	}

	if !strings.Contains(js, "loadRestart") || !strings.Contains(js,
		`const banner = $('#restart-banner');`) {
		t.Error("the loader does not drive the banner")
	}

	// The permission, decided where the banner is filled rather than inherited
	// from a screen it no longer lives on.
	loader := js[strings.Index(js, "async function loadRestart()"):]
	loader = loader[:strings.Index(loader, "\n}\n")]

	if !strings.Contains(loader, "can('settings:manage')") {
		t.Error("the restart banner is filled without asking whether the reader may " +
			"administer this installation")
	}
}

// The footer says which platform the build is running on, beside the version.
//
// The same version is published for four platforms and they do not all behave
// alike, so "v1.0" alone does not say what somebody is looking at - which is the
// first question of a support conversation.
func TestTheFooterNamesThePlatformBesideTheVersion(t *testing.T) {
	js := asset(t, "/app.js")

	if !strings.Contains(js, "branding.os") {
		t.Error("the footer never reads the platform the server reports")
	}

	if !strings.Contains(js, "${version} (${platform})") {
		t.Error(`the footer does not render the platform as "version (platform)"`)
	}
}

// What a container is offered depends on whether anything can replace its image.
//
// Swapping the binary inside a container works and does not last: it changes
// that container and not the image it was made from, so the next recreate
// brings the old version back - and a recreate is how a container deployment
// applies anything at all. An update that reverts on a day nobody connects to
// the button they pressed is worse than no button, so there is none, and the
// card says which command to run instead.
//
// With deploy/compose.update.yaml beside it there is something that can replace
// the image, and then the button is the whole update: a new image, a container
// recreated from it, the old image removed.
func TestAContainerIsOfferedTheImageOrToldToUpdateByHand(t *testing.T) {
	js := asset(t, "/app.js")

	if !strings.Contains(js, "if (state.byImage) {") {
		t.Error("the card does not offer a container with an updater the image")
	}

	if !strings.Contains(js, "if (!state.installable) {") {
		t.Error("a container with nothing to replace its image is offered a button " +
			"whose effect the next recreate undoes")
	}

	// And it names the overlay, because "update it by hand" is only half an
	// answer when the other half is a file in this repository.
	if !strings.Contains(js, "deploy/compose.update.yaml") {
		t.Error("the card tells a container to update by hand without mentioning " +
			"what would let it do so from here")
	}
}

// The image update waits rather than asking for a restart it is not performing.
//
// This is the one that made an update look like nothing happening. The press
// used to fall through to the restart below it: the POST stopped the
// application, its restart policy started the same container again from the
// same old image, and the updater's recreate arrived into the middle of that.
// Whichever won came back, which was usually the version that was already
// there.
func TestTheImageUpdateDoesNotAlsoAskForARestart(t *testing.T) {
	js := asset(t, "/app.js")

	press := js[strings.Index(js, "function wireUpdate()"):]
	press = press[:strings.Index(press, "\n}\n")]

	byImage := strings.Index(press, "if (state?.byImage) {")
	restart := strings.Index(press, "'/settings/restart'")

	if byImage < 0 {
		t.Fatal("the press does not tell an image update from a restart")
	}

	if restart >= 0 && restart < byImage {
		t.Error("the press asks for a restart before it has decided whether " +
			"something else is already replacing this container")
	}

	if !strings.Contains(press, "IMAGE_UPDATE_TIMEOUT_MS") {
		t.Error("the image update is given the same patience as a restart, and it " +
			"has a download in front of it")
	}
}
