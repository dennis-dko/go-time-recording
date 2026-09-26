//go:build integration

package integration

import (
	"bytes"
	"encoding/base64"
	"os"
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

// A start refused by the application's own start-up step exits as a failure.
//
// The step that checks SECRET_KEY against what the installation's secrets were
// written with runs inside GoFr's start hooks, and a hook that fails makes GoFr's
// Run return rather than exit. Run was the last thing main did, so the process
// ended with 0: the systemd unit OPERATIONS.md ships restarts on failure, and it
// read a refused start as a clean stop - which, for a database that was only
// briefly away, is an outage nothing brings back.
func TestAStartTheApplicationRefusesExitsAsAFailure(t *testing.T) {
	t.Parallel()

	if os.Getenv(harness.DSNEnv) != "" {
		t.Skip("this test shares a SQLite file between two instances")
	}

	shared := harness.SharedDatabase(t)
	key := func(b byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
	}

	// The first start records which key the secrets are written with.
	start(t, "DB_NAME="+shared, "SECRET_KEY="+key('a'))

	code, log := harness.StartExpectingExit(t, "DB_NAME="+shared, "SECRET_KEY="+key('b'))

	if code == 0 {
		t.Errorf("a start refused for the wrong key exited with 0, which a supervisor "+
			"reads as a clean stop:\n%s", log)
	}

	if !strings.Contains(log, "SECRET_KEY is not the key") {
		t.Errorf("the refusal did not say why:\n%s", log)
	}
}
