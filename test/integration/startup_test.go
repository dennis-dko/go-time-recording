//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// An instance that cannot start is seen to have stopped, as soon as it stops.
//
// The harness looked for an exit through the process state, which only Wait
// fills in - and nothing called Wait until the case was over. So a start that
// died in its first second was reported a minute later as one that "did not
// become ready", which is the message for a slow machine rather than for a
// refusal, and every case built on it paid the minute.
func TestAnInstanceThatCannotStartIsSeenToExit(t *testing.T) {
	t.Parallel()

	began := time.Now()

	code, log := harness.StartExpectingExit(t, "SECRET_KEY=not a key at all")

	if took := time.Since(began); took > 30*time.Second {
		t.Errorf("the refusal took %s to be noticed", took.Round(time.Second))
	}

	if code == 0 {
		t.Errorf("the refused start exited with 0, which a supervisor reads as success")
	}

	if !strings.Contains(log, "SECRET_KEY cannot be used") {
		t.Errorf("the refusal did not say why:\n%s", log)
	}
}
