package test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// Everything that stops the application gives it longer than it takes to stop.
//
// On a stop the application waits for the requests under way for up to
// SHUTDOWN_GRACE_PERIOD, GoFr's Run waits for that wait to finish, and only then
// does main stop the HTTPS front end, which it gives tlsShutdownGrace. Whatever
// stops it waits its own time before killing it: systemd's TimeoutStopSec, which
// the unit in the operations guide sets with exactly this reason, and Docker's
// stop timeout, which neither compose.yaml nor the guide's docker run set at
// all - so the container was killed at the daemon's default, ten seconds or
// less, while the application meant to wait thirty, and an import under way at
// an update or a restart of the stack was cut off.
func TestEverythingThatStopsTheApplicationWaitsLongerThanItsGrace(t *testing.T) {
	t.Parallel()

	grace := durationIn(t, filepath.Join("..", "cmd", "configs", ".env"),
		regexp.MustCompile(`(?m)^SHUTDOWN_GRACE_PERIOD=(\S+)$`), "") +
		durationIn(t, filepath.Join("..", "cmd", "main.go"),
			regexp.MustCompile(`(?m)^const tlsShutdownGrace = (\d+) \* time\.Second$`), "s")

	for _, bound := range []struct {
		what string
		file string
		in   *regexp.Regexp
		unit string
	}{
		{"the compose service's stop_grace_period", filepath.Join("..", "deploy", "compose.yaml"),
			regexp.MustCompile(`(?m)^    stop_grace_period: (\S+)$`), ""},
		{"the systemd unit's TimeoutStopSec", filepath.Join("..", "deploy", "OPERATIONS.md"),
			regexp.MustCompile(`(?m)^TimeoutStopSec=(\S+)$`), ""},
		{"the bare container's --stop-timeout", filepath.Join("..", "deploy", "OPERATIONS.md"),
			regexp.MustCompile(`(?m)^docker run .*--stop-timeout (\d+)`), "s"},
	} {
		if got := durationIn(t, bound.file, bound.in, bound.unit); got <= grace {
			t.Errorf("%s is %s, no longer than the %s the application may take to stop: "+
				"the requests under way, then its HTTPS front end", bound.what, got, grace)
		}
	}
}

// durationIn reads the one duration a pattern finds in a file, in the unit given
// where the file writes a bare number.
func durationIn(t *testing.T, file string, in *regexp.Regexp, unit string) time.Duration {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	match := in.FindSubmatch(raw)
	if match == nil {
		t.Fatalf("%s sets no %s", file, in)
	}

	value, err := time.ParseDuration(string(match[1]) + unit)
	if err != nil {
		t.Fatalf("%s: %q is not a duration: %v", file, match[1], err)
	}

	return value
}
