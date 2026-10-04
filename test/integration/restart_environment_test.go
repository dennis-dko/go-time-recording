//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A restart from the button starts on what a cold start would read.
//
// Outside a container the process replaces itself, and what it hands on is an
// environment. By the time the button is pressed that is no longer the one the
// process was started with: GoFr has written every key of the configuration file
// into it, the stored settings have been exported into it, and main has widened
// the log level in it. Handed on as it stood, all of that reached the next
// process as real environment variables - which beat the file they came from.
// The three cases below are the three things that followed.

// frontPage is the status the interface itself answers with, asked without a
// session: the document is public, and whether it is served at all is a setting.
func frontPage(t *testing.T, a *app) int {
	t.Helper()

	response, err := http.Get(a.BaseURL() + "/")
	if err != nil {
		t.Fatalf("asking for the front page: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	return response.StatusCode
}

// An edit to the configuration file is read by the restart that follows it.
//
// Every key the file held at the previous start had become a variable of the
// process, so the edited file lost to its own earlier value - on this restart
// and on every one after it, until somebody stopped the process and started it
// again. UI_ENABLED is what is edited here because the file is the only place it
// can be set, and because what it decides can be asked from outside.
func TestARestartFromTheButtonReadsTheConfigurationFileAgain(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	if status := frontPage(t, a); status != http.StatusOK {
		t.Fatalf("the interface answers %d before anything was edited, want it served", status)
	}

	file := filepath.Join(a.Dir(), "configs", ".env")

	shipped, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading the instance's configuration file: %v", err)
	}

	edited := strings.Replace(string(shipped), "UI_ENABLED=true", "UI_ENABLED=false", 1)
	if edited == string(shipped) {
		t.Fatal("the configuration file no longer sets UI_ENABLED=true, so this case " +
			"edits nothing; pick another key the file sets")
	}

	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatalf("editing the instance's configuration file: %v", err)
	}

	restartFromTheButton(t, a, admin)

	if status := frontPage(t, a); status != http.StatusNotFound {
		t.Errorf("the configuration file was edited to switch the interface off, and "+
			"after a restart from the button it answers %d, want %d: the restart did "+
			"not read the file", status, http.StatusNotFound)
	}
}

// A restart does not leave the log more talkative than the configuration says.
//
// Where the output is captured, main widens LOG_LEVEL to DEBUG so the sink has
// everything to filter from. Inherited, that widening was read by the next
// process as the configured level: an installation that had stored no level of
// its own logged at DEBUG from its first restart on, and "follow the
// configuration file" meant DEBUG from then on as well.
func TestARestartDoesNotWidenTheLogLevel(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	a := start(t, "LOG_LEVEL=WARN")
	admin := a.signInAsAdmin("a-much-better-password")

	if level := readTelemetry(t, admin).Active.LogLevel; level != "WARN" {
		t.Fatalf("started at WARN and the level in force reads %q", level)
	}

	restartFromTheButton(t, a, admin)

	if level := readTelemetry(t, admin).Active.LogLevel; level != "WARN" {
		t.Errorf("nothing was stored, and after a restart from the button the level in "+
			"force is %q, want the configured WARN", level)
	}
}

// A setting cleared back to "follow the configuration file" is restored by the
// restart the card then asks for.
//
// The card lists it as waiting, which is true: the next start should serve the
// metrics again. But the value the stored setting had exported was inherited, so
// the restart changed nothing - and then the card went quiet, because the
// inherited value had become what "the configuration file says". The switch was
// back on, the endpoint stayed off, and nothing was waiting any more.
func TestASettingClearedBackToTheFileIsRestoredByTheButton(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	metrics := fmt.Sprintf("http://localhost:%d/metrics", a.MetricsPort())

	served := func() bool {
		response, err := http.Get(metrics)
		if err != nil {
			return false
		}

		defer func() { _ = response.Body.Close() }()

		return response.StatusCode == http.StatusOK
	}

	if !served() {
		t.Fatal("the metrics are not served before anything was changed")
	}

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"metricsOff": true}), http.StatusOK)

	restartFromTheButton(t, a, admin)

	if served() {
		t.Fatal("the metrics were switched off and are still served after the restart")
	}

	// Back to following the configuration, which serves them.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{}), http.StatusOK)

	if _, _, waiting := restartState(t, admin).pendingFor("metrics"); !waiting {
		t.Fatal("the switch was cleared and the card does not list the metrics as waiting")
	}

	after := restartFromTheButton(t, a, admin)

	if !served() {
		t.Error("the switch was cleared, the card asked for a restart, and after it " +
			"the metrics are still not served")
	}

	if len(after.Pending) != 0 {
		t.Errorf("after the restart the card still lists %+v", after.Pending)
	}
}
