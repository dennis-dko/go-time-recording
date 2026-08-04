//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Everything else about tracing can be checked without a collector: that the
// settings are stored, that they are refused when GoFr could not use them, that
// they reach the process before gofr.New(). None of that shows a span arriving
// anywhere, and every way that fails is silent - an exporter GoFr does not
// recognise drops each batch after logging once, a collector address it cannot
// dial fails inside the exporter, a sampler that records nothing looks exactly
// like a collector with nothing to show.
//
// So this one exports to a real Jaeger and reads the trace back out of it.
//
//	docker compose --profile traces up -d --wait      (from test/)
//	GTR_TEST_JAEGER=localhost:54317 go test -tags integration ./test/integration
//
// Or `task test:traces`, which does both.

const (
	// jaegerEnv holds the collector as host:port - what the application dials.
	// Empty skips, like the database DSN: a collector is not something every
	// checkout has running.
	jaegerEnv = "GTR_TEST_JAEGER"

	// jaegerQueryEnv overrides where the trace is read back from. It has a
	// default because test/docker-compose.yml fixes the port.
	jaegerQueryEnv     = "GTR_TEST_JAEGER_QUERY"
	defaultJaegerQuery = "http://localhost:51686"
)

// jaegerQueryURL is where the exported trace is read back from.
func jaegerQueryURL() string {
	if custom := os.Getenv(jaegerQueryEnv); custom != "" {
		return custom
	}

	return defaultJaegerQuery
}

// requireJaeger skips unless a collector is configured and answering.
//
// Answering as well as configured: a stale environment variable pointing at a
// container somebody stopped would otherwise fail this test for a reason that
// has nothing to do with the application.
func requireJaeger(t *testing.T) string {
	t.Helper()

	collector := os.Getenv(jaegerEnv)
	if collector == "" {
		t.Skipf("%s is not set; start test/docker-compose.yml's traces profile to run this", jaegerEnv)
	}

	conn, err := net.DialTimeout("tcp", collector, 5*time.Second)
	if err != nil {
		t.Skipf("%s is %q but nothing answers there: %v", jaegerEnv, collector, err)
	}

	_ = conn.Close()

	return collector
}

// tracedServices asks Jaeger which services it has seen spans from.
//
// The service name is the one thing that identifies this application's traces,
// and GoFr takes it from APP_NAME - so this is also, incidentally, a check that
// the name travels with the spans rather than arriving as "unknown_service".
func tracedServices(t *testing.T) []string {
	t.Helper()

	var body struct {
		Data []string `json:"data"`
	}

	raw := get(t, jaegerQueryURL()+"/api/services")

	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("cannot read the Jaeger service list from %q: %v", truncate(raw, 200), err)
	}

	return body.Data
}

// The whole point of the feature, proved against something that stores spans:
// what an administrator saves is what the next start exports with, and the spans
// arrive.
func TestTracingConfiguredFromTheScreenActuallyExportsSpans(t *testing.T) {
	collector := requireJaeger(t)

	if dsn := os.Getenv(harness.DSNEnv); dsn != "" {
		t.Skip("this test shares a SQLite file between two instances")
	}

	shared := filepath.Join(t.TempDir(), "shared")

	// A name of this test's own, so the trace read back cannot be one left in
	// the collector by an earlier run or by the staging profile.
	service := fmt.Sprintf("gtr-integration-%d", time.Now().UnixNano())

	first := start(t, "DB_NAME="+shared, "APP_NAME="+service)
	admin := first.signInAsAdmin("a-much-better-password")

	// Configured the way an administrator would: through the screen, with no
	// tracing in the environment at all.
	admin.must(admin.api(http.MethodPut, "/settings/telemetry", map[string]any{
		"traceExporter": "otlp",
		"tracerUrl":     collector,
		"tracerRatio":   1,
	}), http.StatusOK)

	// Nothing has been exported yet: this process started before the setting
	// existed, which is the whole reason it takes a restart.
	if exporter := readTelemetry(t, admin).Active.TraceExporter; exporter != "" {
		t.Errorf("the first instance is exporting to %q before any restart", exporter)
	}

	second := start(t, "DB_NAME="+shared, "APP_NAME="+service)

	// A request to trace. Any request will do - the middleware spans every one.
	fresh := second.newClient()
	fresh.signIn(adminEmail, "a-much-better-password")
	fresh.must(fresh.api(http.MethodGet, "/me", nil), http.StatusOK)

	// The exporter batches, so the span is not in the collector the moment the
	// request finishes. Asserting immediately would be asserting against the
	// batch processor's timer.
	if !eventuallyWithin(45*time.Second, func() bool {
		return contains(tracedServices(t), service)
	}) {
		t.Errorf("no span from %q reached the collector at %s; Jaeger knows only %v\n\n"+
			"application log:\n%s",
			service, collector, tracedServices(t), truncate(second.Log(), 2000))
	}
}

// And the other direction, which is the one an administrator is more likely to
// need: tracing configured in the environment, switched off from the screen, and
// then nothing more arrives.
func TestSwitchingTracingOffStopsSpansReachingTheCollector(t *testing.T) {
	collector := requireJaeger(t)

	if dsn := os.Getenv(harness.DSNEnv); dsn != "" {
		t.Skip("this test shares a SQLite file between two instances")
	}

	shared := filepath.Join(t.TempDir(), "shared")
	service := fmt.Sprintf("gtr-off-%d", time.Now().UnixNano())

	// This instance traces because its environment says so, the way a deployment
	// configured in a file would.
	first := start(t, "DB_NAME="+shared, "APP_NAME="+service,
		"TRACE_EXPORTER=otlp", "TRACER_URL="+collector, "TRACER_RATIO=1")

	admin := first.signInAsAdmin("a-much-better-password")

	// It really is exporting, or switching it off proves nothing.
	if !eventuallyWithin(45*time.Second, func() bool {
		return contains(tracedServices(t), service)
	}) {
		t.Fatalf("the first instance never exported a span to %s, so there is nothing to switch off",
			collector)
	}

	admin.must(admin.api(http.MethodPut, "/settings/telemetry",
		map[string]any{"traceExporter": ""}), http.StatusOK)

	// Same environment, same collector, and a stored off. The assertion is on
	// the process rather than on the collector: Jaeger keeps what it already has,
	// so "no new spans" cannot be read from a service list that still lists this
	// one.
	second := start(t, "DB_NAME="+shared, "APP_NAME="+service,
		"TRACE_EXPORTER=otlp", "TRACER_URL="+collector, "TRACER_RATIO=1")

	fresh := second.newClient()
	fresh.signIn(adminEmail, "a-much-better-password")

	if exporter := readTelemetry(t, fresh).Active.TraceExporter; exporter != "" {
		t.Errorf("the second instance still exports to %q despite the stored off", exporter)
	}
}

// eventuallyWithin is eventually with a deadline of its own.
//
// The shared one allows ten seconds, which is right for the caches it was
// written for. A batched span exporter is a different order of patience: the
// batch timer alone is five seconds, and the first gRPC connection through a
// container's published port can take several more.
func eventuallyWithin(limit time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(limit)

	for time.Now().Before(deadline) {
		if condition() {
			return true
		}

		time.Sleep(500 * time.Millisecond)
	}

	return false
}
