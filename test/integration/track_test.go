//go:build integration

package integration

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The binary books from a terminal as the owner of a personal token: track book
// puts an entry on the owner's project, and the stopwatch track starts is the one
// the API shows running, which track stop then books - the same clock, not a
// second one kept on the machine. The token is never written back out.
func TestTheBinaryBooksFromATerminalWithAPersonalToken(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	var website projectResponse

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Website", "startDate": "2026-10-01",
	}), http.StatusCreated, http.StatusOK).Data(t, &website)

	var token struct {
		Secret string `json:"secret"`
	}

	worker.must(worker.api(http.MethodPost, "/me/tokens", map[string]any{"name": "terminal"}),
		http.StatusCreated, http.StatusOK).Data(t, &token)

	track := func(args ...string) string {
		t.Helper()

		cmd := exec.Command(harness.Binary(), append([]string{"track"}, args...)...)
		cmd.Env = append(os.Environ(), "GTR_URL="+a.BaseURL(), "GTR_TOKEN="+token.Secret)

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("track %s: %v\n%s", strings.Join(args, " "), err, out)
		}

		if strings.Contains(string(out), token.Secret) {
			t.Errorf("track %s wrote the token out", strings.Join(args, " "))
		}

		return string(out)
	}

	if out := track("book", "2,5", "web", "-m", "from the terminal", "-d", "2026-10-05"); !strings.Contains(out,
		"Booked 2.50 h on Website for 2026-10-05.") {
		t.Errorf("track book said %q", out)
	}

	var entries listOf[timesheetResponse]

	worker.must(worker.api(http.MethodGet, "/timesheets?from=2026-10-05&to=2026-10-05", nil), http.StatusOK).
		Data(t, &entries)

	if len(entries.Items) != 1 || entries.Items[0].DurationHours != 2.5 || entries.Items[0].ProjectID == nil ||
		*entries.Items[0].ProjectID != website.ID || entries.Items[0].Description == nil ||
		*entries.Items[0].Description != "from the terminal" {
		t.Errorf("the day holds %+v, want the 2.5 hours booked from the terminal on Website", entries.Items)
	}

	track("start", "Website")

	// Half an hour back, through the API the browser uses, because a stopwatch
	// stopped after a few seconds books nothing anybody could have meant.
	worker.must(worker.api(http.MethodPut, "/me/timer", map[string]any{
		"startedAt": time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
	}), http.StatusOK)

	if out := track("status"); !strings.Contains(out, "on Website - 0:30 so far.") {
		t.Errorf("track status said %q, want the clock on Website at half an hour", out)
	}

	if out := track("stop"); !strings.Contains(out, "Booked 0.50 h on Website.") {
		t.Errorf("track stop said %q, want half an hour booked on Website", out)
	}

	var clock struct {
		Running bool `json:"running"`
	}

	worker.must(worker.api(http.MethodGet, "/me/timer", nil), http.StatusOK).Data(t, &clock)

	if clock.Running {
		t.Error("the stopwatch is still running after track stop")
	}
}
