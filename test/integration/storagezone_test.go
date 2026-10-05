//go:build integration

package integration

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// A MySQL installation refuses to start in another zone than its times were
// written in, and starts again in its own.
//
// GoFr opens MySQL with loc=Local, and a moment is kept there in a DATETIME,
// which has no zone: a day is stored as the clock of the writing process read at
// midnight UTC, and read back through the zone of the reading one. Measured: a
// day written by a process in UTC was read by one in Europe/Berlin as the day
// before, and that day's totals did not find it. Setting TZ on a container is all
// it takes, so the first start records the zone and a start in another stops
// rather than moving every entry.
func TestAMySQLInstallationRefusesToStartInAnotherZone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Go reads TZ only on Unix; run this in a Linux container")
	}

	env := harness.SharedServerDatabase(t)
	if !slices.Contains(env, "DB_DIALECT=mysql") {
		t.Skip("only MySQL keeps a moment without its zone; needs " + harness.DSNEnv + " on a MySQL server")
	}

	written := harness.Start(t, append(slices.Clone(env), "TZ=UTC")...)
	written.Stop(t)

	code, log := harness.StartExpectingExit(t, append(slices.Clone(env), "TZ=Europe/Berlin")...)
	if code == 0 {
		t.Errorf("a start in another zone than the database was written in exited 0:\n%s", log)
	}

	for _, said := range []string{"UTC", "Europe/Berlin", "+00:00/+00:00", "+01:00/+02:00"} {
		if !strings.Contains(log, said) {
			t.Errorf("the refusal does not say %q:\n%s", said, log)
		}
	}

	again := harness.Start(t, append(slices.Clone(env), "TZ=UTC")...)
	again.Stop(t)
}
