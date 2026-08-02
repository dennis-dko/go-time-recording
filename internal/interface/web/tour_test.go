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
func TestTheTourYieldsToTheSetupWizard(t *testing.T) {
	js := asset(t, "/app.js")

	start := strings.Index(js, "async function maybeStartTour()")
	if start < 0 {
		t.Fatal("could not find maybeStartTour in app.js")
	}

	body := js[start : start+500]

	if !strings.Contains(body, "#setup-wizard") {
		t.Error("the tour must not start while the setup wizard is showing")
	}

	if !strings.Contains(body, "tourSeen") {
		t.Error("the tour must only be offered to someone who has not seen it")
	}
}
