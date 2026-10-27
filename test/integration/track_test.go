//go:build integration

package integration

import (
	"errors"
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
		cmd.Env = append(os.Environ(), "GTR_URL="+a.BaseURL(), "GTR_TOKEN="+token.Secret, "GTR_LANG=en")

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

// Asked for German, the binary says a refusal in the words the German web
// interface has for it - from the code and the values the installation sent, so
// the day in it is written the German way rather than the way it travels.
func TestTheBinarySaysARefusalInTheBrowsersGerman(t *testing.T) {
	t.Parallel()

	a, _, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)
	worker.must(worker.api(http.MethodPut, path("/users/", me.User.ID, "/working-times"),
		map[string]any{"dailyTargetHours": 8, "maxDailyHours": 8}), http.StatusOK)
	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Website", "startDate": "2026-10-01",
	}), http.StatusCreated, http.StatusOK)

	var token struct {
		Secret string `json:"secret"`
	}

	worker.must(worker.api(http.MethodPost, "/me/tokens", map[string]any{"name": "terminal"}),
		http.StatusCreated, http.StatusOK).Data(t, &token)

	track := func(args ...string) (string, int) {
		t.Helper()

		cmd := exec.Command(harness.Binary(), append([]string{"track", "--lang", "de"}, args...)...)
		cmd.Env = append(os.Environ(), "GTR_URL="+a.BaseURL(), "GTR_TOKEN="+token.Secret)

		out, err := cmd.CombinedOutput()

		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return string(out), exited.ExitCode()
		}

		if err != nil {
			t.Fatalf("track %s: %v", strings.Join(args, " "), err)
		}

		return string(out), 0
	}

	if out, status := track("book", "5", "web", "-d", "2026-10-06"); status != 0 ||
		!strings.Contains(out, "5,00 Std. auf Website für den 06.10.2026 gebucht.") {
		t.Errorf("the first five hours answered %d %q", status, out)
	}

	want := "5 Std. würden am 06.10.2026 zusammen 10 Std. ergeben und damit das Tagesmaximum von 8 Std. überschreiten."
	if out, status := track("book", "5", "web", "-d", "2026-10-06"); status != 1 || !strings.Contains(out, want) {
		t.Errorf("five more hours on the same day answered %d %q, want 1 and %q", status, out, want)
	}
}
