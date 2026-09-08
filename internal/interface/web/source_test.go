package web_test

import (
	"regexp"
	"testing"
)

// Where a top-level declaration begins, which is where the one above it ends.
var nextDeclaration = regexp.MustCompile(`(?m)^(async function |function |const |// -----)`)

// functionSource returns the source of one top-level function in app.js.
//
// Three cases cut a function out of the file for themselves, each in its own
// way, and the third way was the one that broke: a fixed nine hundred
// characters after the declaration, which reached past the function it was
// about into the next one but one - so a case asserting something about endTour
// failed when three lines were added to putTheTourAway, and said that ending
// the tour no longer records it as seen.
//
// A body ends where the next declaration at the top level begins. That is a
// fact about the file rather than a number somebody has to keep in step with
// it.
func functionSource(t *testing.T, js, name string) string {
	t.Helper()

	start := regexp.MustCompile(`(?m)^(?:async )?function ` + regexp.QuoteMeta(name) + `\(`).
		FindStringIndex(js)
	if start == nil {
		t.Fatalf("app.js no longer declares %s; this test is reading nothing", name)
	}

	end := nextDeclaration.FindStringIndex(js[start[1]:])
	if end == nil {
		return js[start[0]:]
	}

	return js[start[0] : start[1]+end[0]]
}
