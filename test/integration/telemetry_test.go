//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

	// Waited for rather than read once. The log reaches this test through the
	// process's own output and a pipe drained by a goroutine, so a request having
	// returned says nothing about its lines having been written, flushed, read and
	// appended. Reading immediately passed locally and on three CI runs before
	// failing on the fourth, which is what a race looks like while it is still
	// losing slowly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(a.log()[before:], `"level":"DEBUG"`) {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

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

// The update card answers on every deployment, and only to the account that may
// act on it.
//
// It is the one thing on the administration screen guarded harder than
// settings:manage. Everything else there changes what the application does; this
// changes what it *is* - the bytes that will be executed after the next start.
func TestTheUpdateCheckAnswersAndIsGuarded(t *testing.T) {
	t.Parallel()

	// A feed of our own, because the point is the answer's shape and not whether
	// GitHub is up - and a test that reaches the internet is a test that fails on
	// a train.
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","body":"much better",` +
			`"html_url":"https://example.invalid/r","assets":[]}`))
	}))
	defer feed.Close()

	a := start(t, "UPDATE_FEED="+feed.URL)
	admin := a.signInAsAdmin("a-much-better-password")

	var state struct {
		Running     string `json:"running"`
		Latest      string `json:"latest"`
		Newer       bool   `json:"newer"`
		Available   bool   `json:"available"`
		Installable bool   `json:"installable"`
		Enabled     bool   `json:"enabled"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/update", nil), http.StatusOK).Data(t, &state)

	if !state.Enabled {
		t.Error("the check reports itself switched off although nothing switched it off")
	}

	if state.Latest != "v99.0.0" {
		t.Errorf("the feed said v99.0.0 and the answer says %q", state.Latest)
	}

	// The harness builds without a tag, so this process calls itself "dev" - and
	// "dev" cannot be compared with anything. That is deliberate: an update
	// offered on the strength of a version nobody can read is worse than one not
	// offered.
	if state.Running != "dev" {
		t.Logf("this build calls itself %q", state.Running)
	}

	// Whatever the comparison said, a release that published no binary for this
	// platform is never installable.
	if state.Available {
		t.Error("a release with no assets at all is offered as installable")
	}

	// And no class of installation is turned away in advance.
	//
	// This used to be false in a container, so the card printed a docker command
	// instead of offering a button - which left the deployment this application
	// ships with no way to update from its own interface. A binary swapped inside
	// a container is undone the next time the container is recreated, which is
	// true and is a caveat rather than a refusal: a restart of the same container
	// keeps it, and that is what the shipped restart policy does.
	//
	// Asked here rather than in a container, which no test in this suite runs in:
	// what can be checked is that the answer no longer depends on it.
	if !state.Installable {
		t.Error("this installation is told it cannot install an update at all, " +
			"before any release has been looked at")
	}

	// Everybody who may configure this installation may also update it, including
	// somebody who administers and books their own time.
	//
	// It was narrower once - the built-in administrator, or an account that
	// administers and records nothing - on the argument that replacing the bytes
	// that will be executed is a different kind of decision from changing a
	// setting. What settled it the other way: the screen is reached by holding
	// settings:manage, everything else on it belongs to whoever got there, and a
	// card that appears for some of those people and not others is a rule nobody
	// can hold in their head. Anyone who could reach it could tick the narrower
	// right onto a role for themselves in any case.
	both := a.signInAsWorkingAdmin(admin, "Bothe", "bothe@example.com")

	if got := both.api(http.MethodGet, "/settings/update", nil).Status; got != http.StatusOK {
		t.Errorf("somebody who administers and also books time cannot see the "+
			"version: %d, want %d", got, http.StatusOK)
	}

	// Refused for want of anything to install rather than for who is asking:
	// this feed publishes no binary for this platform.
	if got := both.api(http.MethodPost, "/settings/update", nil).Status; got == http.StatusForbidden {
		t.Error("the same account is refused the update it can see")
	}

	// Somebody who only works here is refused, which is the boundary that has
	// not moved.
	worker := a.signInAsUser(admin, "Wera", "wera@example.com")

	if got := worker.api(http.MethodGet, "/settings/update", nil).Status; got != http.StatusForbidden {
		t.Errorf("an ordinary account reached the update check: %d, want %d",
			got, http.StatusForbidden)
	}

	// And installing is refused when there is nothing newer to install, rather
	// than downloading whatever the feed happens to name.
	if got := admin.api(http.MethodPost, "/settings/update", nil).Status; got == http.StatusOK {
		t.Error("an update was installed from a release with no binary for this platform")
	}
}

// Switched off means switched off: no lookup, and no install.
func TestUpdatingCanBeSwitchedOff(t *testing.T) {
	t.Parallel()

	reached := false

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer feed.Close()

	a := start(t, "UPDATE_CHECK=false", "UPDATE_FEED="+feed.URL)
	admin := a.signInAsAdmin("a-much-better-password")

	var state struct {
		Enabled bool   `json:"enabled"`
		Latest  string `json:"latest"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/update", nil), http.StatusOK).Data(t, &state)

	if state.Enabled {
		t.Error("UPDATE_CHECK=false and the answer says the check is on")
	}

	if state.Latest != "" {
		t.Errorf("the feed was asked anyway, and answered %q", state.Latest)
	}

	if reached {
		t.Error("an installation that asked not to call out called out")
	}

	if got := admin.api(http.MethodPost, "/settings/update", nil).Status; got == http.StatusOK {
		t.Error("an update was installed although checking is switched off")
	}
}

// Asking by hand gets past the answer the card is holding on to.
//
// The automatic check keeps what the feed said for six hours, so an
// administrator who has just published a release is told the version before it
// and has no way to say "look again". This is that way, and the thing worth
// asserting is exactly the difference: the feed changes its mind, the ordinary
// GET does not notice, and the manual check does.
func TestAManualCheckSeesAReleaseTheCachedAnswerMissed(t *testing.T) {
	t.Parallel()

	var latest atomic.Value

	latest.Store("v99.0.0")

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"body":"much better",`+
			`"html_url":"https://example.invalid/r","assets":[]}`, latest.Load())
	}))
	defer feed.Close()

	a := start(t, "UPDATE_FEED="+feed.URL)
	admin := a.signInAsAdmin("a-much-better-password")

	var state struct {
		Latest string `json:"latest"`
	}

	// The first look fills the cache.
	admin.must(admin.api(http.MethodGet, "/settings/update", nil), http.StatusOK).Data(t, &state)

	if state.Latest != "v99.0.0" {
		t.Fatalf("the feed said v99.0.0 and the card says %q", state.Latest)
	}

	// A release is published while the card is holding the old answer.
	latest.Store("v99.1.0")

	admin.must(admin.api(http.MethodGet, "/settings/update", nil), http.StatusOK).Data(t, &state)

	if state.Latest != "v99.0.0" {
		t.Fatalf("the cache is not being used, so this test proves nothing; got %q", state.Latest)
	}

	// The button.
	admin.must(admin.api(http.MethodPost, "/settings/update/check", nil),
		http.StatusCreated).Data(t, &state)

	if state.Latest != "v99.1.0" {
		t.Errorf("asking by hand still returned the cached answer: %q", state.Latest)
	}

	// And the card keeps the fresh answer afterwards, rather than the manual ask
	// being a one-off that the next load undoes.
	admin.must(admin.api(http.MethodGet, "/settings/update", nil), http.StatusOK).Data(t, &state)

	if state.Latest != "v99.1.0" {
		t.Errorf("the fresh answer was not kept; the card fell back to %q", state.Latest)
	}
}

// The button has a limit of its own, or it hands back the problem the cache
// exists to prevent.
func TestAManualCheckCannotBeUsedToHammerTheFeed(t *testing.T) {
	t.Parallel()

	var asked atomic.Int64

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)

		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0","body":"much better",` +
			`"html_url":"https://example.invalid/r","assets":[]}`))
	}))
	defer feed.Close()

	a := start(t, "UPDATE_FEED="+feed.URL)
	admin := a.signInAsAdmin("a-much-better-password")

	if got := admin.api(http.MethodPost, "/settings/update/check", nil).Status; got != http.StatusCreated {
		t.Fatalf("the first manual check was refused with %d", got)
	}

	before := asked.Load()

	// Pressed again straight away, several times, as an impatient person does.
	for range 5 {
		if got := admin.api(http.MethodPost, "/settings/update/check", nil).Status; got != http.StatusConflict {
			t.Errorf("a second ask moments later should be refused, got %d", got)
		}
	}

	if after := asked.Load(); after != before {
		t.Errorf("the feed was contacted %d more time(s) despite the refusals", after-before)
	}
}

// And it is guarded like everything else on this card.
func TestAManualCheckNeedsToBeAnAdministrator(t *testing.T) {
	t.Parallel()

	a, admin, _ := startWithWorker(t)
	worker := a.signInAsUser(admin, "Nina", "nina@example.com")

	if got := worker.api(http.MethodPost, "/settings/update/check", nil).Status; got != http.StatusForbidden {
		t.Errorf("somebody who books time may ask the release feed: got %d", got)
	}
}

// A directory that is not switched on yet still has to come back with a role
// somebody can be given.
//
// The check that the default role exists runs only when the directory is
// enabled, which is the wrong way round for how the card is filled in: it is
// saved repeatedly while it is still being configured, and switched on last.
// An empty role saved during that gets stored, and the stored empty value then
// beats the default the reader has - so the picker comes back with nothing
// selected, and saving again writes the empty value once more.
func TestADirectoryThatIsOffStillNamesARoleForNewAccounts(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// The card as it is part way through: switched off, and the role picker
	// showing nothing because nothing was chosen.
	admin.must(admin.api(http.MethodPut, "/settings/ldap", map[string]any{
		"enabled":        false,
		"host":           "",
		"port":           389,
		"userFilter":     "(|(uid=%s)(mail=%s))",
		"nameAttribute":  "cn",
		"emailAttribute": "mail",
		"idAttribute":    "entryUUID",
		"defaultRole":    "",
	}), http.StatusOK)

	// The response embeds the request, so the field sits at the top level rather
	// than under an "ldap" key - reading it in the wrong place answers "" for
	// every installation and would make this case pass for nothing.
	var saved struct {
		DefaultRole string `json:"defaultRole"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/ldap", nil), http.StatusOK).Data(t, &saved)

	if saved.DefaultRole == "" {
		t.Fatal("the directory came back with no default role, so the picker on the " +
			"card has nothing selected and the next save stores the emptiness again")
	}

	// And it has to be a role that exists, or accounts the directory creates are
	// unusable.
	var roles listOf[struct {
		Name string `json:"name"`
	}]

	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	found := false

	for _, role := range roles.Items {
		if role.Name == saved.DefaultRole {
			found = true
		}
	}

	if !found {
		t.Errorf("the default role %q is not one of the roles that exist", saved.DefaultRole)
	}
}
