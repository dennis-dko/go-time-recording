//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// There is one wait for the handover, and only one.
//
// The installer serves on the same port the application will, mounts its page at
// "/" and therefore answers every path - so deciding "has the application taken
// over yet" is a question with a wrong answer readily available, and every case
// that gets it wrong fails somewhere else entirely, seconds later, on a port that
// has closed in between.
//
// Two cases asked it. One asked through waitForTheApplication and one had its own
// copy, and when the shared one was corrected the copy was left behind - so the
// fix held for one case and the other went on failing, in CI, on a different job,
// as "GET /: EOF". One bug in two places, half-fixed, is worse than either half:
// it reads as a flake that was already dealt with.
//
// So the question has one implementation, and this is what keeps it that way.
func TestOnlyTheSharedWaitAsksWhetherTheApplicationHasTakenOver(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	var elsewhere []string

	for _, name := range sources {
		// Not itself: this file names the thing it is looking for.
		if name == "handover_test.go" {
			continue
		}

		body, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("reading %s: %v", name, readErr)
		}

		for i, line := range strings.Split(string(body), "\n") {
			// tryGet is the marker, not the path. tryGet swallows a refused
			// connection and answers "", which is what a poll needs and what makes
			// it a poll - a case that merely reads the branding once uses get(),
			// which fails the test if the request does not work, and is asking a
			// different question entirely.
			if !polls.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}

			if name == "install_test.go" && withinTheSharedWait(string(body), i+1) {
				continue
			}

			elsewhere = append(elsewhere, name+":"+strings.TrimSpace(line))
		}
	}

	for _, at := range elsewhere {
		t.Errorf("%s polls the application awake outside waitForTheApplication. That is the question "+
			"the installer can answer wrongly - its page is served for every path - "+
			"so a second copy of the test for it is a second chance to accept the "+
			"installer and hand back a port that is about to close. Call the shared "+
			"wait", at)
	}
}

// polls matches a loop asking whether the application is up yet.
var polls = regexp.MustCompile(`tryGet\(.*branding`)

// withinTheSharedWait reports whether that line is inside waitForTheApplication.
func withinTheSharedWait(body string, line int) bool {
	lines := strings.Split(body, "\n")

	start := -1

	for i, at := range lines {
		if regexp.MustCompile(`^func waitForTheApplication\(`).MatchString(at) {
			start = i + 1

			break
		}
	}

	if start < 0 {
		return false
	}

	// To the next top-level declaration.
	for i := start; i < len(lines); i++ {
		if i+1 > line && regexp.MustCompile(`^(func |// )`).MatchString(lines[i]) {
			return false
		}

		if i+1 == line {
			return true
		}
	}

	return false
}
