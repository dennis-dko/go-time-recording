//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"
)

// The settings that are only read at start-up - the database connection, the log
// level, the metrics port, the trace exporter - are administered from a screen
// and then wait. Asking somebody to find a shell to finish the job is most of
// the way to not having administered them at all.
//
// What is waiting is worked out on the server, because it is the same comparison
// the interface would otherwise make twice and be wrong in one of them.

// RestartState mirrors the response.
type RestartState struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
	StartedAt string `json:"startedAt"`

	Pending []struct {
		Setting string `json:"setting"`
		Running string `json:"running"`
		Stored  string `json:"stored"`
	} `json:"pending"`
}

func restartState(t *testing.T, admin *client) RestartState {
	t.Helper()

	var state RestartState

	admin.must(admin.api(http.MethodGet, "/settings/restart", nil), http.StatusOK).Data(t, &state)

	return state
}

// pendingFor finds a setting in the list, so a test can say what it expects
// rather than index into an order the server does not promise.
func (s RestartState) pendingFor(setting string) (running, stored string, found bool) {
	for _, change := range s.Pending {
		if change.Setting == setting {
			return change.Running, change.Stored, true
		}
	}

	return "", "", false
}

// tryRestartState reads the state without failing the test when nothing answers.
//
// The ordinary client calls t.Fatalf on a refused connection, which is right for
// every other test here and exactly wrong for this one: a refused connection is
// what the middle of a restart looks like, and waiting for it to stop being
// refused is the whole assertion. The session cookie has to travel, so this goes
// through the client's own jar rather than the anonymous helper.
func (c *client) tryRestartState() (RestartState, bool) {
	response, err := c.http.Get(c.app.BaseURL() + "/api/v1/settings/restart")
	if err != nil {
		return RestartState{}, false
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return RestartState{}, false
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return RestartState{}, false
	}

	var envelope struct {
		Data RestartState `json:"data"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return RestartState{}, false
	}

	return envelope.Data, true
}

// A freshly started instance is running exactly what is stored, so there is
// nothing to report and the card stays out of the way.
func TestNothingIsPendingOnAnInstanceThatWasJustStarted(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if state := restartState(t, admin); len(state.Pending) > 0 {
		t.Errorf("a fresh instance reports %d pending change(s): %+v", len(state.Pending), state.Pending)
	}
}

// Saving a start-up setting has to show up as pending, naming both what is
// running and what was saved - a list that said only "trace exporter" would
// leave the administrator to remember what they changed it from.
//
// The log level used to be this test's example and is no longer a start-up
// setting at all: it is applied to the running process by the log sink, so it
// has nothing to wait for. See TestTheLogLevelAppliesWithoutARestart, and
// TestTheLogLevelIsNotWaitingForAnything below for the other half of that.
func TestSavingAStartUpSettingIsReportedAsPending(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// The exporter and its collector travel together: an exporter with nowhere to
	// send to is refused on the way in, which is a different rule and has its own
	// test.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"traceExporter": "otlp", "tracerUrl": "collector:4317"}),
		http.StatusOK)

	running, stored, found := restartState(t, admin).pendingFor("traceExporter")
	if !found {
		t.Fatal("a saved trace exporter is not reported as waiting for a restart")
	}

	if running != "" || stored != "otlp" {
		t.Errorf("reported %q -> %q, want \"\" -> otlp", running, stored)
	}

	// Storing what the process is already running with is not a pending change,
	// or the card would sit there forever telling nobody anything.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"traceExporter": ""}), http.StatusOK)

	if _, _, still := restartState(t, admin).pendingFor("traceExporter"); still {
		t.Error("storing the exporter already in force is reported as pending")
	}
}

// The log level is never waiting for anything, whatever it is set to.
//
// It was the most common reason this card appeared: somebody turns on DEBUG to
// look at something and is told to restart the application they are diagnosing.
// The sink applies it on the way out now, so by the time the card could report
// it, it is already in force - and a card listing a change that has already
// happened is worse than one that says nothing.
func TestTheLogLevelIsNotWaitingForAnything(t *testing.T) {
	t.Parallel()

	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")

	for _, level := range []string{"DEBUG", "ERROR", "INFO", ""} {
		admin.must(admin.api(http.MethodPut, "/settings/telemetry",
			map[string]any{"logLevel": level}), http.StatusOK)

		if _, _, found := restartState(t, admin).pendingFor("logLevel"); found {
			t.Errorf("setting the level to %q is reported as waiting for a restart, "+
				"after it has already been applied", level)
		}
	}
}

// Several at once, because that is the normal case: somebody opens the screen
// and sets what they came to set.
func TestEveryWaitingSettingIsListed(t *testing.T) {
	t.Parallel()

	a := start(t, "LOG_LEVEL=WARN")
	admin := a.signInAsAdmin("a-much-better-password")

	admin.must(admin.api(http.MethodPut, "/settings/telemetry", map[string]any{
		"logLevel":      "DEBUG",
		"metricsOff":    true,
		"traceExporter": "otlp",
		"tracerUrl":     "collector:4317",
		"tracerRatio":   0.25,
	}), http.StatusOK)

	state := restartState(t, admin)

	// The share was missing from both the save and the list it is checked
	// against, which is how it stayed missing: this case is called "every waiting
	// setting" and was asking about three of the four. It goes into the exporter
	// with the two above it, and the exporter is built while the process starts.
	//
	// The log level is saved in the same request and is deliberately not here: it
	// is the one of these that is already in force.
	for _, setting := range []string{"metrics", "traceExporter", "tracerUrl", "tracerRatio"} {
		if _, _, found := state.pendingFor(setting); !found {
			t.Errorf("%q was saved but is not reported as waiting: %+v", setting, state.Pending)
		}
	}

	if _, _, found := state.pendingFor("logLevel"); found {
		t.Errorf("the log level is listed as waiting beside settings that really "+
			"are: %+v", state.Pending)
	}
}

// The switch that only goes one way: metrics can be turned off from here, so
// only "on now, off after the restart" can ever be pending.
func TestSwitchingMetricsOffIsPendingButLeavingThemOnIsNot(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if _, _, found := restartState(t, admin).pendingFor("metrics"); found {
		t.Fatal("metrics are reported as pending before anything was changed")
	}

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"metricsOff": true}), http.StatusOK)

	running, stored, found := restartState(t, admin).pendingFor("metrics")
	if !found {
		t.Fatal("switching the metrics endpoint off is not reported as waiting")
	}

	if running != "on" || stored != "off" {
		t.Errorf("reported %q -> %q, want on -> off", running, stored)
	}
}

// Whether the button is offered has to match what the platform can actually do,
// or it is a button that fails on click.
func TestTheRestartButtonIsOfferedOnlyWhereItWouldWork(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	state := restartState(t, admin)

	// Windows has no execve, so a process cannot replace its own image and the
	// screen says so instead of offering to try.
	if runtime.GOOS == "windows" {
		if state.Supported {
			t.Error("a restart is offered on Windows, where the process cannot replace itself")
		}

		if state.Reason == "" {
			t.Error("the refusal does not say why")
		}

		return
	}

	if !state.Supported {
		t.Errorf("a restart is not offered on %s: %s", runtime.GOOS, state.Reason)
	}
}

// The one that proves the feature rather than the plumbing: the process really
// is replaced, it comes back, and it comes back running what was stored.
func TestRestartingAppliesWhatWasWaiting(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	a := start(t, "LOG_LEVEL=WARN")
	admin := a.signInAsAdmin("a-much-better-password")

	before := restartState(t, admin)

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"logLevel": "DEBUG"}), http.StatusOK)

	admin.must(admin.api(http.MethodPost, "/settings/restart", nil),
		http.StatusCreated, http.StatusOK)

	// A different process answering is the only honest signal that it happened:
	// replacing the image takes milliseconds, so waiting for the port to close
	// and open again would miss it entirely and report success for nothing.
	var after RestartState

	if !eventuallyWithin(45*time.Second, func() bool {
		state, ok := admin.tryRestartState()
		if !ok {
			return false
		}

		after = state

		return after.StartedAt != "" && after.StartedAt != before.StartedAt
	}) {
		t.Fatalf("the application did not come back as a different process\n\napplication log:\n%s",
			truncate(a.log(), 2000))
	}

	// And it came back running the setting, which is the whole point - not
	// merely running again.
	if _, _, found := after.pendingFor("logLevel"); found {
		t.Error("the log level is still waiting for a restart after one")
	}

	// The session survived, because it lives in the database rather than in the
	// process that was replaced. An administrator being signed out by their own
	// restart would be a poor reward for pressing the button.
	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK)
}
