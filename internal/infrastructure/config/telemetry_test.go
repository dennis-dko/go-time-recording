package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// These settings reach GoFr through the process environment, which is the only
// way in: GoFr binds the metrics port and builds the trace exporter inside
// gofr.New(), before this application has a say in anything. What is checked here
// is therefore not "was a field copied" but the two properties the arrangement
// stands on - that an administered value beats the configuration file, and that a
// value nobody administered is left entirely alone.

func ptr[T any](v T) *T { return &v }

// givenEnvironment sets the keys to what a configuration file would have supplied,
// so a test can tell "left alone" from "overwritten with the same thing".
func givenEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("LOG_LEVEL", "INFO")
	t.Setenv("METRICS_PORT", "2121")
	t.Setenv("TRACE_EXPORTER", "jaeger")
	t.Setenv("TRACER_URL", "from-the-file:4317")
	t.Setenv("TRACER_RATIO", "0.25")
}

// Nothing administered has to leave every value where it was. The opposite would
// mean the first save from the Settings screen silently switched off tracing for
// every installation that had configured it in a file.
func TestApplyingNothingLeavesTheFileAlone(t *testing.T) {
	givenEnvironment(t)

	if err := config.ApplyTelemetry(model.Telemetry{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for key, want := range map[string]string{
		"LOG_LEVEL":      "INFO",
		"METRICS_PORT":   "2121",
		"TRACE_EXPORTER": "jaeger",
		"TRACER_URL":     "from-the-file:4317",
		"TRACER_RATIO":   "0.25",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s became %q; an unadministered setting must keep the file's %q", key, got, want)
		}
	}
}

// The metrics endpoint is switched off with "0" and not with an empty value.
// GoFr reads this one with GetOrDefault, which treats empty as absent - so an
// empty value would fall through to its default port and switch the endpoint on
// instead of off.
func TestSwitchingMetricsOffWritesTheDisablingPortRatherThanNothing(t *testing.T) {
	givenEnvironment(t)

	if err := config.ApplyTelemetry(model.Telemetry{MetricsOff: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := os.Getenv("METRICS_PORT"); got != "0" {
		t.Errorf("METRICS_PORT is %q, want \"0\" - anything else leaves the endpoint served", got)
	}
}

// The property the whole override depends on: GoFr snapshots the real environment
// before it loads its .env files and writes that snapshot back afterwards, so a
// variable has to be *present* to win. A variable set to an empty string is
// present; an unset one is not, and the file would win instead.
func TestSwitchingTracingOffLeavesAnEmptyButPresentVariable(t *testing.T) {
	givenEnvironment(t)

	err := config.ApplyTelemetry(model.Telemetry{TraceExporter: ptr(model.TracingOff)})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, key := range []string{"TRACE_EXPORTER", "TRACER_URL"} {
		value, present := os.LookupEnv(key)
		if !present {
			t.Errorf("%s is unset, so the configuration file's value would win", key)
		}

		if value != "" {
			t.Errorf("%s is %q, want it emptied", key, value)
		}

		if !inEnvironment(key) {
			t.Errorf("%s is missing from os.Environ(), which is what GoFr snapshots", key)
		}
	}
}

// The collector address is cleared with the exporter. Left behind it would meet an
// empty exporter, which GoFr reports as "missing TRACE_EXPORTER config" on every
// start - an error line about a setting somebody deliberately switched off.
func TestSwitchingTracingOffClearsTheCollectorFromTheFileToo(t *testing.T) {
	givenEnvironment(t)

	err := config.ApplyTelemetry(model.Telemetry{TraceExporter: ptr(model.TracingOff)})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := os.Getenv("TRACER_URL"); got != "" {
		t.Errorf("TRACER_URL is still %q", got)
	}
}

func TestAnAdministeredExporterReplacesTheFile(t *testing.T) {
	givenEnvironment(t)

	err := config.ApplyTelemetry(model.Telemetry{
		TraceExporter: ptr(model.TraceExporterOTLP),
		TracerURL:     ptr("collector:4317"),
		TracerRatio:   ptr(0.5),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	for key, want := range map[string]string{
		"TRACE_EXPORTER": "otlp",
		"TRACER_URL":     "collector:4317",
		"TRACER_RATIO":   "0.5",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s is %q, want %q", key, got, want)
		}
	}
}

// GoFr parses the ratio with ParseFloat and, on failure, carries on with the zero
// value - which samples nothing. So the exported form has to be one ParseFloat
// reads: no exponent worth arguing about, and above all no decimal comma.
func TestTheSamplingRatioIsWrittenInAFormGoFrCanParse(t *testing.T) {
	cases := map[float64]string{
		0:      "0",
		0.0001: "0.0001",
		0.5:    "0.5",
		1:      "1",
	}

	for given, want := range cases {
		t.Setenv("TRACER_RATIO", "")

		if err := config.ApplyTelemetry(model.Telemetry{TracerRatio: ptr(given)}); err != nil {
			t.Fatalf("apply: %v", err)
		}

		got := os.Getenv("TRACER_RATIO")
		if got != want {
			t.Errorf("a ratio of %v was written as %q, want %q", given, got, want)
		}

		if strings.ContainsAny(got, ",eE") {
			t.Errorf("a ratio of %v was written as %q, which ParseFloat may not read as intended", given, got)
		}
	}
}

// An administered level replaces the file's; an empty one is the absence it
// means, and must not be exported as a blank - GoFr reads a blank as INFO, so a
// cleared field would quietly set a level rather than stop setting one.
func TestTheLogLevelIsExportedOnlyWhenItSaysSomething(t *testing.T) {
	givenEnvironment(t)

	if err := config.ApplyTelemetry(model.Telemetry{LogLevel: ptr("debug")}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := os.Getenv("LOG_LEVEL"); got != "DEBUG" {
		t.Errorf("LOG_LEVEL is %q, want it upper-cased to DEBUG", got)
	}

	givenEnvironment(t)

	if err := config.ApplyTelemetry(model.Telemetry{LogLevel: ptr("")}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := os.Getenv("LOG_LEVEL"); got != "INFO" {
		t.Errorf("an empty level changed LOG_LEVEL to %q; it must leave the file's value alone", got)
	}
}

// mapConfig is GoFr's config reader reduced to what Load needs, including its
// treatment of an empty value as absent - which is the behaviour several of the
// resolutions below turn on.
type mapConfig map[string]string

func (m mapConfig) Get(key string) string { return m[key] }

func (m mapConfig) GetOrDefault(key, fallback string) string {
	if value := m[key]; value != "" {
		return value
	}

	return fallback
}

// The screen reports what this process is actually doing, so these have to be
// GoFr's own rules rather than sensible ones.
func TestTheReportedTelemetryFollowsGoFrsOwnRules(t *testing.T) {
	cases := map[string]struct {
		given       mapConfig
		wantPort    int
		wantServed  bool
		wantRatio   float64
		wantTracing bool
	}{
		"nothing configured serves metrics on GoFr's default port and exports nothing": {
			mapConfig{}, 2121, true, 1, false,
		},
		"the literal zero is the only thing that switches the endpoint off": {
			mapConfig{"METRICS_PORT": "0"}, 0, false, 1, false,
		},
		"a port GoFr cannot read falls back to its default rather than switching off": {
			mapConfig{"METRICS_PORT": "off"}, 2121, true, 1, false,
		},
		"a negative port falls back too": {
			mapConfig{"METRICS_PORT": "-1"}, 2121, true, 1, false,
		},
		"a configured port is reported as it stands": {
			mapConfig{"METRICS_PORT": "9100"}, 9100, true, 1, false,
		},
		// The silent one: GoFr logs the parse error and samples nothing.
		"a ratio GoFr cannot read means nothing is sampled, not everything": {
			mapConfig{"TRACE_EXPORTER": "otlp", "TRACER_URL": "c:4317", "TRACER_RATIO": "half"},
			2121, true, 0, true,
		},
		"a ratio above one is clamped by the sampler": {
			mapConfig{"TRACER_RATIO": "2"}, 2121, true, 1, false,
		},
		"a negative ratio is clamped to sampling nothing": {
			mapConfig{"TRACER_RATIO": "-1"}, 2121, true, 0, false,
		},
		// ParseFloat reads this one happily, and it compares false against every
		// bound. Left in, it reaches the settings response, which JSON has no way
		// to write - so one hand-edited file would leave the screen blank.
		"a ratio of NaN is reported as sampling nothing": {
			mapConfig{"TRACER_RATIO": "NaN"}, 2121, true, 0, false,
		},
		"an exporter in any case is still that exporter": {
			mapConfig{"TRACE_EXPORTER": " OTLP ", "TRACER_URL": "c:4317"}, 2121, true, 1, true,
		},
	}

	for name, tc := range cases {
		got := config.Load(tc.given).Telemetry

		if got.MetricsPort != tc.wantPort {
			t.Errorf("%s: metrics port %d, want %d", name, got.MetricsPort, tc.wantPort)
		}

		if got.MetricsServed() != tc.wantServed {
			t.Errorf("%s: metrics served %v, want %v", name, got.MetricsServed(), tc.wantServed)
		}

		if got.TracerRatio != tc.wantRatio {
			t.Errorf("%s: ratio %v, want %v", name, got.TracerRatio, tc.wantRatio)
		}

		if got.TracingEnabled() != tc.wantTracing {
			t.Errorf("%s: tracing enabled %v, want %v", name, got.TracingEnabled(), tc.wantTracing)
		}
	}
}

func inEnvironment(key string) bool {
	for _, entry := range os.Environ() {
		if name, _, found := strings.Cut(entry, "="); found && name == key {
			return true
		}
	}

	return false
}
