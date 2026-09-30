package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// A failed request reaches the corner of the screen through toastFailure, never
// through toast with its message alone.
//
// An internal failure is answered with a sentence for the reader and, beside it,
// the original text and a reference for whoever fixes it; the German sentence
// says the technical details are underneath. toast takes those as its third
// argument, and two callers passed them while nine did not - the install, the
// update check, deleting an account, an export, a restart, three loaders and the
// start of the page - so on each of those the sentence promised details that were
// not there. toastFailure is the one place that builds the message and the detail
// together, which is what makes the next caller unable to drop the second half.
func TestAFailedRequestIsToastedWithItsDetail(t *testing.T) {
	script := readAsset(t, "app.js")

	direct := regexp.MustCompile(`toast\([^;]*err\.message`)

	for i, line := range strings.Split(script, "\n") {
		if direct.MatchString(line) {
			t.Errorf("app.js:%d toasts a failure's message without its detail; use toastFailure: %s",
				i+1, strings.TrimSpace(line))
		}
	}

	if !strings.Contains(script, "function toastFailure(") {
		t.Error("app.js has no toastFailure, so nothing puts a failure's detail under its sentence")
	}
}
