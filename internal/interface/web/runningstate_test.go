package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// What the installation is running is written before any form is asked about.
//
// Three cards on the administration screen say what is actually in force: the
// connection this process is using, what it is serving and exporting, and the
// limits it is running under. Each sits beside a form that changes those things,
// and each loader guards the form with beingEdited so a background load does not
// take away what somebody is typing.
//
// The guard is right and it is only about the boxes. Where the running state was
// written after it, one touch of the form froze it: the datasource card went on
// saying "connected via postgres" above five empty boxes, the telemetry card
// went on naming the metrics address of a process that had since restarted, and
// the operational card went on showing the figures somebody was weighing their
// own against.
//
// So this checks the order rather than the presence: the line is written before
// the loader can return. A guard after it is fine and there is one - the
// telemetry placeholders are the form's, and wait for it.
func TestWhatIsRunningIsWrittenBeforeAnyFormGuard(t *testing.T) {
	js := asset(t, "/app.js")

	for _, line := range []struct {
		id   string
		what string
	}{
		{"datasource-active", "the connection this process is using"},
		{"telemetry-active", "what this process is serving and exporting"},
		{"operational-effective", "the limits this process is running under"},
	} {
		written := strings.Index(js, "$('#"+line.id+"')")
		if written < 0 {
			t.Errorf("app.js no longer writes #%s, so %s is not being checked",
				line.id, line.what)

			continue
		}

		start := strings.LastIndex(js[:written], "\nfunction ")
		if start < 0 {
			t.Fatalf("could not find the function that writes #%s", line.id)
		}

		guard := regexp.MustCompile(`if \(beingEdited\([^)]*\)\) return;`).
			FindStringIndex(js[start:written])
		if guard == nil {
			continue
		}

		t.Errorf("#%s says %s and is written after a beingEdited return in the same "+
			"function, so one touch of the form beside it freezes the line for as "+
			"long as the tab stays open", line.id, line.what)
	}
}
