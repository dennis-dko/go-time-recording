//go:build integration

package integration

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
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

// A start whose port is taken ends, and says which port.
//
// GoFr checks both ports it is about to claim - by dialling them - and answers one
// that is in use with a Fatal. A Fatal goes through the captured output and is
// gone before anything reads it, so a second copy started by mistake, or a stale
// one still holding the port, exited 1 with a warning about SECRET_KEY as the only
// line it ever wrote: a start that ends without saying why, on the ordinary way
// to reach it. The log package's own description already said the application
// checks a taken port before the framework is asked to; it checked the database
// and nothing else.
//
// Held on every interface, the way the port would be held by another copy of
// this application.
func TestAStartOnATakenPortEndsAndNamesThePort(t *testing.T) {
	for _, setting := range []string{"HTTP_PORT", "METRICS_PORT"} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()

			taken, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Fatalf("cannot hold a port for the case: %v", err)
			}

			t.Cleanup(func() { _ = taken.Close() })

			port := strconv.Itoa(taken.Addr().(*net.TCPAddr).Port)

			code, log := harness.StartExpectingExit(t, fmt.Sprintf("%s=%s", setting, port))

			if code == 0 {
				t.Errorf("a start that could not serve exited with 0, which a supervisor " +
					"reads as a clean stop")
			}

			if !strings.Contains(log, port) {
				t.Errorf("the refusal does not name port %s:\n%s", port, log)
			}
		})
	}
}

// Stopping an installation that is still waiting for its installer is not a
// failure.
//
// A container stopped, or a service taken down, before anybody chose a database
// is the ordinary end of a process nobody has configured yet. The signal ended
// the wait as its comment intends, and main then died on it - "cannot run the
// installer: context canceled", exit status 1 - so a service manager recorded a
// failed unit for a stop it had asked for itself, and the last line in the log
// read as an error. The application answers the same signal with 0 once it is
// running.
func TestStoppingAWaitingInstallerIsNotAFailure(t *testing.T) {
	t.Parallel()

	a := harness.StartUnconfigured(t)

	if code := a.Stop(t); code != 0 {
		t.Errorf("stopping the installer ended with status %d, which a service manager "+
			"reads as a failure", code)
	}

	if strings.Contains(a.Log(), "cannot run the installer") {
		t.Errorf("a requested stop is logged as the installer failing:\n%s", a.Log())
	}
}
