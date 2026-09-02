package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// A write that succeeded is not reported as a write that failed.
//
// mutate wrapped both halves of a mutation in one try: the call itself, and the
// `after` that reloads the screen behind it. So a save that worked and a reload
// that then failed came out the same way - a red toast carrying whatever the
// *read* said, immediately after the green one saying the save had worked.
//
// refreshAll is the `after` most callers pass, and it awaits a dozen loads with
// no handling of its own, so any one of them failing rejects it. The two other
// places that call refreshAll already treat that as its own condition, with its
// own sentence: "Could not load everything". mutate was the one place that
// conflated it with the write failing.
//
// What that costs is a retry. This application reasons about exactly that hazard
// where it decides not to abort a write mid-flight - "somebody would do it again,
// which for an import means writing every row twice" - and telling somebody a
// completed save failed invites the same thing.
//
// Checked as source rather than by driving it, and that is a real limit worth
// stating: making a reload fail on demand, after a mutation has succeeded, is not
// something the browser suite can arrange without a fault injector this project
// does not have. What is checkable is that the two failures have two sentences,
// which is the whole of the fix.
func TestASuccessfulWriteIsNotReportedAsAFailedOne(t *testing.T) {
	body := mutateBody(t, asset(t, "/app.js"))

	if !strings.Contains(body, "msg.loadFailed") {
		t.Error("mutate reports a failed reload with the same wording as a failed " +
			"write, so a save that worked is shown as a save that did not - and the " +
			"obvious response to that is to do it again. The reload has its own " +
			"sentence already, msg.loadFailed, used by both other callers of " +
			"refreshAll")
	}

	// And the success is still announced before the reload is attempted, or a slow
	// reload would hold back the one word confirming the save landed.
	success := strings.Index(body, "successMessage")
	reload := strings.Index(body, "await after(")

	if success < 0 || reload < 0 {
		t.Fatalf("mutate no longer looks like itself; this test is reading the "+
			"wrong thing:\n%s", body)
	}

	if success > reload {
		t.Error("mutate awaits the reload before announcing the save, so the " +
			"confirmation waits on work that is not part of it")
	}
}

// mutateBody returns the source of the mutate function.
func mutateBody(t *testing.T, js string) string {
	t.Helper()

	start := regexp.MustCompile(`(?m)^async function mutate\(`).FindStringIndex(js)
	if start == nil {
		t.Fatal("app.js no longer declares mutate")
	}

	// To the next top-level declaration, which is where the function ends.
	rest := js[start[1]:]

	end := regexp.MustCompile(`(?m)^(async function |function |const |// -----)`).
		FindStringIndex(rest)
	if end == nil {
		t.Fatal("could not find the end of mutate")
	}

	return js[start[0] : start[1]+end[0]]
}
