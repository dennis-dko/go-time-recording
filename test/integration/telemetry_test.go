//go:build integration

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The metrics endpoint and the trace exporter are the two settings this
// application cannot apply to itself: GoFr binds the one and builds the other
// inside gofr.New(), before there is a screen to administer either from. So they
// are stored and read back out of the database on the way into the *next* start,
// which is a seam no unit test can reach - the whole claim is about what a second
// process does with what the first one wrote.
//
// That is what the last test here checks, by starting a second instance against
// the same database. The rest guard the way in: values GoFr would accept and then
// fail on quietly have to be refused while somebody is still looking at the
// screen.

// TelemetryOnTheWire mirrors the response shape.
type TelemetryOnTheWire struct {
	Configured struct {
		LogLevel      *string  `json:"logLevel"`
		MetricsOff    bool     `json:"metricsOff"`
		TraceExporter *string  `json:"traceExporter"`
		TracerURL     *string  `json:"tracerUrl"`
		TracerRatio   *float64 `json:"tracerRatio"`
	} `json:"configured"`

	Active struct {
		LogLevel      string  `json:"logLevel"`
		MetricsServed bool    `json:"metricsServed"`
		MetricsPort   int     `json:"metricsPort"`
		MetricsPath   string  `json:"metricsPath"`
		TraceExporter string  `json:"traceExporter"`
		TracerURL     string  `json:"tracerUrl"`
		TracerRatio   float64 `json:"tracerRatio"`
	} `json:"active"`

	RestartRequired bool `json:"restartRequired"`
}

func readTelemetry(t *testing.T, c *client) TelemetryOnTheWire {
	t.Helper()

	var state TelemetryOnTheWire

	c.must(c.api(http.MethodGet, "/settings/telemetry", nil), http.StatusOK).Data(t, &state)

	return state
}

// A fresh installation has administered nothing, and the screen has to say what
// the process is actually doing rather than leaving it to be guessed from a file
// nobody can read from there.
//
// Read as the built-in administrator, because the metrics port and the collector
// address are part of running the installation rather than anybody's working day,
// and nobody else may see them - which the next test is about.
func TestTelemetryReportsWhatTheProcessIsActuallyDoing(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	state := readTelemetry(t, admin)

	if state.Configured.TraceExporter != nil {
		t.Errorf("nothing was administered, but the exporter reads as %q", *state.Configured.TraceExporter)
	}

	// The harness gives every instance its own metrics port, so this instance is
	// serving them - and the screen has to name the port and path, which is the
	// one thing on it somebody wants to copy.
	if !state.Active.MetricsServed || state.Active.MetricsPort <= 0 {
		t.Errorf("metrics are served on port %d, reported as served=%v",
			state.Active.MetricsPort, state.Active.MetricsServed)
	}

	if state.Active.MetricsPath != "/metrics" {
		t.Errorf("the metrics path is reported as %q, want /metrics", state.Active.MetricsPath)
	}

	// Tracing is off unless a collector was configured, and reporting otherwise
	// would send somebody looking for spans that were never exported.
	if state.Active.TraceExporter != "" {
		t.Errorf("nothing configured tracing, but it reports exporter %q", state.Active.TraceExporter)
	}
}

// The metrics port carries no authentication and serves Go's profiling endpoints
// beside the metrics, so who may read and change these settings matters as much
// as the settings themselves.
//
// It is a right now rather than the built-in account by name. This used to check
// that nobody but that one account could reach it - the strictest rule available,
// and the reason people signed in as the built-in administrator to do ordinary
// administration, which is the one account whose actions cannot be attributed to a
// person. What guards it now is settings:manage, held by the roles that administer
// and by nobody else. See Authorizer.RequireInstallationAdmin, which records what
// that costs.
func TestTelemetryNeedsTheRightToManageSettings(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	for _, account := range []map[string]any{
		{"name": "Second admin", "email": "admin2@example.com",
			"role": "admin", "password": "admin2-password-1"},
		{"name": "Worker", "email": "worker@example.com",
			"role": "user", "password": "worker-password-1"},
	} {
		admin.must(admin.api(http.MethodPost, "/users", account),
			http.StatusCreated, http.StatusOK)
	}

	// Somebody who administers reaches it without being the built-in account.
	other := a.newClient()
	other.signIn("admin2@example.com", "admin2-password-1")

	if got := other.api(http.MethodGet, "/settings/telemetry", nil).Status; got != http.StatusOK {
		t.Errorf("an account holding the administrator role was refused telemetry: %d", got)
	}

	if got := other.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"metricsOff": true}).Status; !accepted(got) {
		t.Errorf("an account holding the administrator role could not change telemetry: %d", got)
	}

	// And somebody who only records time does not, which is the half that matters.
	worker := a.newClient()
	worker.signIn("worker@example.com", "worker-password-1")

	if got := worker.api(http.MethodGet, "/settings/telemetry", nil).Status; got == http.StatusOK {
		t.Error("an account that only records time could read the telemetry settings")
	}

	if got := worker.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"metricsOff": true}).Status; got == http.StatusOK {
		t.Error("an account that only records time could change the telemetry settings")
	}
}

// Every value below is one GoFr accepts and then fails on without saying so: an
// unknown exporter drops each span, a collector address with a scheme fails inside
// the exporter, and a ratio it cannot parse samples nothing. Refusing them here is
// the only point at which somebody is still looking.
func TestTelemetryRefusesWhatWouldFailSilently(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	cases := map[string]map[string]any{
		"an exporter GoFr does not know":  {"traceExporter": "otel", "tracerUrl": "c:4317"},
		"zipkin, which is on its way out": {"traceExporter": "zipkin", "tracerUrl": "z:9411"},
		"the hosted gofr exporter":        {"traceExporter": "gofr"},
		"an exporter with no collector":   {"traceExporter": "otlp"},
		"a collector with a scheme":       {"traceExporter": "otlp", "tracerUrl": "http://jaeger:4317"},
		"a collector with a path":         {"traceExporter": "otlp", "tracerUrl": "jaeger:4317/v1/traces"},
		"a collector with no port":        {"traceExporter": "otlp", "tracerUrl": "jaeger"},
		"a ratio above one":               {"tracerRatio": 1.5},
		"a negative ratio":                {"tracerRatio": -1},
	}

	for name, body := range cases {
		response := admin.api(http.MethodPut, "/settings/telemetry", body)

		if response.Status == http.StatusOK {
			t.Errorf("%s was accepted: %s", name, response.Body)

			continue
		}

		// The refusal has to name what to correct, or the administrator is left
		// guessing which of four fields the server disliked.
		if response.Message() == "" {
			t.Errorf("%s was refused without a message", name)
		}
	}

	// And nothing was stored on the way through.
	if state := readTelemetry(t, admin); state.Configured.TraceExporter != nil {
		t.Errorf("a refused save still stored the exporter %q", *state.Configured.TraceExporter)
	}
}

// Saving says plainly that it takes effect at the next start, and reads back what
// was actually stored rather than echoing the request: the collector address is
// trimmed on the way in, and switching tracing off clears it.
func TestSavingTelemetrySaysItAppliesAtTheNextStart(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var saved TelemetryOnTheWire

	admin.must(admin.api(http.MethodPut, "/settings/telemetry", map[string]any{
		"traceExporter": "otlp",
		"tracerUrl":     "  collector:4317  ",
		"tracerRatio":   0.25,
	}), http.StatusOK).Data(t, &saved)

	if !saved.RestartRequired {
		t.Error("the save did not report that a restart is needed, which is the whole shape of this setting")
	}

	if saved.Configured.TracerURL == nil || *saved.Configured.TracerURL != "collector:4317" {
		t.Errorf("the collector address was not trimmed on the way in: %v", saved.Configured.TracerURL)
	}

	// This process is still exporting nothing, and the screen must not pretend
	// otherwise while the administrator is looking at their own saved settings.
	if saved.Active.TraceExporter != "" {
		t.Errorf("the running process reports exporter %q after a mere save", saved.Active.TraceExporter)
	}

	// Switching it off again clears the address, so a collector from a previous
	// exporter cannot read later as a setting in force.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"traceExporter": ""}), http.StatusOK)

	after := readTelemetry(t, admin)

	if after.Configured.TracerURL != nil {
		t.Errorf("the collector address survived switching tracing off: %q", *after.Configured.TracerURL)
	}

	if after.Configured.TraceExporter == nil || *after.Configured.TraceExporter != "" {
		t.Errorf("an administered off must stay administered, got %v", after.Configured.TraceExporter)
	}

	// And the reset puts everything back to following the configuration file,
	// which is a different state again from an administered off.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry", map[string]any{}), http.StatusOK)

	if reset := readTelemetry(t, admin); reset.Configured.TraceExporter != nil {
		t.Errorf("the reset left the exporter administered as %v", reset.Configured.TraceExporter)
	}
}

// The claim the whole feature rests on: what one process stores, the next one
// applies before GoFr can read the configuration file. Nothing short of a second
// process can show it, because the seam being tested runs before gofr.New().
//
// Metrics are the half that proves it out loud: GoFr logs that the endpoint is
// disabled, and the harness has given this instance a real METRICS_PORT in its
// environment - so the stored setting also has to be seen beating that.
func TestWhatWasStoredIsAppliedByTheNextStart(t *testing.T) {
	if dsn := os.Getenv(harness.DSNEnv); dsn != "" {
		// Both instances have to share one database, and on a server the harness
		// gives each of them its own.
		t.Skip("this test shares a SQLite file between two instances")
	}

	// Outside either instance's own directory, so the second one can still open it
	// after the first has been torn down.
	shared := filepath.Join(t.TempDir(), "shared")

	first := start(t, "DB_NAME="+shared)
	admin := first.signInAsAdmin("a-much-better-password")

	if !readTelemetry(t, admin).Active.MetricsServed {
		t.Fatal("the first instance is not serving metrics, so switching them off proves nothing")
	}

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"metricsOff": true}), http.StatusOK)

	// LOG_LEVEL=INFO because the proof is GoFr's own line about it, and the
	// harness otherwise starts instances at WARN, where that line never appears.
	second := start(t, "DB_NAME="+shared, "LOG_LEVEL=INFO")

	// GoFr saying so is the assertion that matters: it proves the framework acted
	// on the stored setting, where reading it back would only prove this
	// application wrote it down.
	if !eventually(func() bool {
		return strings.Contains(second.Log(), "Metrics server is disabled")
	}) {
		t.Errorf("the second instance still serves metrics, so the stored setting was not applied "+
			"before gofr.New():\n%s", truncate(second.Log(), 2000))
	}

	// Read from the second process, which is the one that acted on it. The
	// password was changed on the shared database by the first instance, so this
	// signs in with that one rather than the initial password.
	fresh := second.newClient()
	fresh.signIn(adminEmail, "a-much-better-password")

	state := readTelemetry(t, fresh)

	if state.Active.MetricsServed {
		t.Error("the second instance reports metrics as served after they were switched off")
	}

	if !state.Configured.MetricsOff {
		t.Error("the second instance does not see the stored setting at all")
	}
}

// The mechanism the whole feature rests on, in a real process: an administered
// "off" has to beat a TRACE_EXPORTER that is genuinely set in the environment.
//
// It works because GoFr snapshots the real environment before it loads its files
// and writes that snapshot back afterwards, so an empty value is still a value
// and wins. That is a property of a framework this project does not own, which is
// exactly why it is checked by starting a process rather than by asserting on the
// os.Setenv call - the symptom, if it ever stops holding, is tracing that cannot
// be turned off, and nothing else here would notice.
func TestAnAdministeredOffBeatsAnExporterInTheEnvironment(t *testing.T) {
	if dsn := os.Getenv(harness.DSNEnv); dsn != "" {
		t.Skip("this test shares a SQLite file between two instances")
	}

	shared := filepath.Join(t.TempDir(), "shared")

	first := start(t, "DB_NAME="+shared, "TRACE_EXPORTER=otlp", "TRACER_URL=collector:4317")
	admin := first.signInAsAdmin("a-much-better-password")

	// The environment really did configure tracing, or switching it off proves
	// nothing at all.
	if exporter := readTelemetry(t, admin).Active.TraceExporter; exporter != "otlp" {
		t.Fatalf("the first instance reports exporter %q, want otlp from its environment", exporter)
	}

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"traceExporter": ""}), http.StatusOK)

	// Same environment, same collector, and now a stored off.
	second := start(t, "DB_NAME="+shared, "TRACE_EXPORTER=otlp", "TRACER_URL=collector:4317")

	fresh := second.newClient()
	fresh.signIn(adminEmail, "a-much-better-password")

	if exporter := readTelemetry(t, fresh).Active.TraceExporter; exporter != "" {
		t.Errorf("the second instance is still exporting to %q, so the stored off lost to the "+
			"environment", exporter)
	}
}

// The log level takes effect while the process runs, and says so.
//
// It is the one telemetry setting that does not wait for a restart, and it is
// the one most worth not waiting for: somebody turns on DEBUG to look at
// something that is happening now, and being told to restart the application
// they are diagnosing is being told to make it stop happening. On Windows there
// is no restart button at all - no execve - so "at the next start" meant finding
// a shell.
//
// It works because the level is applied by the log sink rather than by the
// framework's logger, which decides from a field every request goroutine reads
// without synchronisation. This drives the real binary, so what is asserted is
// the whole seam: saved through the API, applied to the capture, visible in the
// process output.
func TestTheLogLevelAppliesWithoutARestart(t *testing.T) {
	a := start(t, "LOG_LEVEL=WARN")
	admin := a.signInAsAdmin("a-much-better-password")

	// Nothing below WARN is being written, which is what the file asked for and
	// what makes the change below visible rather than assumed.
	if strings.Contains(a.log(), `"level":"DEBUG"`) {
		t.Fatal("the process is emitting DEBUG while the file says WARN, so this " +
			"test cannot tell a change from the starting state")
	}

	var saved TelemetryOnTheWire

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"logLevel": "DEBUG"}), http.StatusOK).Data(t, &saved)

	// Both halves of "no restart needed": the screen says so, and the readout of
	// what the process is doing has already moved.
	if saved.RestartRequired {
		t.Error("saving only the log level still asks for a restart")
	}

	if saved.Active.LogLevel != "DEBUG" {
		t.Errorf("the running process reports %q after the save, want DEBUG - the "+
			"screen is describing the level this process started with",
			saved.Active.LogLevel)
	}

	// And the restart card agrees, because it is a second place that could have
	// gone on asking.
	var state struct {
		Pending []struct {
			Setting string `json:"setting"`
		} `json:"pending"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/restart", nil), http.StatusOK).Data(t, &state)

	for _, change := range state.Pending {
		if change.Setting == "logLevel" {
			t.Error("the restart card lists the log level as waiting for a restart " +
				"after it has already been applied")
		}
	}

	// The proof: a request made now produces lines the old level would have
	// dropped. GoFr logs every query at DEBUG, so any authenticated call does.
	before := len(a.log())

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK)

	if !strings.Contains(a.log()[before:], `"level":"DEBUG"`) {
		t.Errorf("no DEBUG line was written after raising the level, so the change "+
			"reached the setting and not the log:\n%.600s", a.log()[before:])
	}

	// Clearing it goes back to what the configuration file said, rather than to
	// a default nobody chose.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"logLevel": ""}), http.StatusOK).Data(t, &saved)

	if saved.Active.LogLevel != "WARN" {
		t.Errorf("clearing the field left the level at %q, want the file's WARN",
			saved.Active.LogLevel)
	}
}
