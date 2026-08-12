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

// The restart card appears when something is waiting for a restart, and not
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
// save that needs a restart brings the card up, and the card puts the reason
// where the button would have been. Nothing is hidden; it is said when it
// matters instead of always.
//
// Checked in the asset rather than in a browser because the browser suite runs on
// Linux, where restarting is supported and this branch is never taken.
func TestTheRestartCardAppearsOnlyWhenSomethingIsWaiting(t *testing.T) {
	js := asset(t, "/app.js")

	if !strings.Contains(js, "card.hidden = pending.length === 0;") {
		t.Error("the restart card is shown on an empty pending list, which makes a " +
			"standing warning out of a screen that should speak up when it has something")
	}

	// And the explanation is translated rather than shown as the server wrote it,
	// keyed on which refusal it is - see TestEveryRestartRefusalIsExplained.
	if !strings.Contains(js, "restart.unsupported.${state.reasonCode") {
		t.Error("the limitation is shown in the server's English wording, or is not " +
			"keyed on which refusal it is")
	}

	// The hint above the list promises saved changes, so it goes when there are
	// none: on Windows the card is permanent, and a standing promise above an
	// empty list reads as a rendering fault.
	if !strings.Contains(js, "$('#restart-hint').hidden = !waiting") {
		t.Error("the card promises a list of pending changes even when there are none")
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
