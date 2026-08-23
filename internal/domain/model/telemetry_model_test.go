package model_test

import (
	"math"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// GoFr fails every one of these settings quietly. An exporter it does not know
// drops each span without a word, a collector address with a scheme in front of
// it fails inside the exporter where nobody is looking, and a sampling ratio it
// cannot read samples nothing at all. From the screen that configured them, all
// three look exactly like working tracing - so the rejection has to happen here,
// on the way in, or it never happens.

// Nothing administered means the configuration file still decides everything,
// and there is nothing to reject.
func TestEmptyTelemetryLeavesTheFileAlone(t *testing.T) {
	var telemetry model.Telemetry

	if invalid := telemetry.InvalidTelemetryFields(); len(invalid) > 0 {
		t.Errorf("nothing was administered, but %v were rejected", invalid)
	}

	if telemetry.TracingAdministered() {
		t.Error("no exporter was administered, so tracing must not count as administered")
	}
}

// Start-up asks this to tell an ordinary first start, where there is nothing to
// read, from a read that failed and lost something.
func TestAdministeredNoticesEachThingOnItsOwn(t *testing.T) {
	if (model.Telemetry{}).Administered() {
		t.Error("an empty setting must not count as administered")
	}

	administered := map[string]model.Telemetry{
		"metrics switched off": {MetricsOff: true},
		"tracing switched off": {TraceExporter: new(model.TracingOff)},
		"an exporter":          {TraceExporter: new(model.TraceExporterOTLP)},
		"a collector only":     {TracerURL: new("jaeger:4317")},
		// Zero is a real choice, so it has to be noticed like any other.
		"a ratio of zero": {TracerRatio: new(0.0)},
	}

	for name, given := range administered {
		if !given.Administered() {
			t.Errorf("%s must count as administered", name)
		}
	}
}

// The distinction the pointers exist for: an exporter of "" is a deliberate off
// that has to beat a value in the file, and a nil exporter must leave it alone.
func TestAnEmptyExporterIsOffRatherThanAbsent(t *testing.T) {
	off := model.Telemetry{TraceExporter: new(model.TracingOff)}

	if off.TraceExporter == nil {
		t.Fatal("an administered off must stay administered")
	}

	if off.TracingAdministered() {
		t.Error("an administered off must not report tracing as administered")
	}

	if invalid := off.InvalidTelemetryFields(); len(invalid) > 0 {
		t.Errorf("switching tracing off must be allowed, but %v were rejected", invalid)
	}
}

// A collector address left behind from a previous exporter would read, later, as
// a setting that is in force.
func TestSwitchingTracingOffClearsWhatItMakesMeaningless(t *testing.T) {
	got := model.Telemetry{
		TraceExporter: new(model.TracingOff),
		TracerURL:     new("jaeger:4317"),
		TracerRatio:   new(0.5),
	}.Normalise()

	if got.TracerURL != nil {
		t.Errorf("the collector address survived switching tracing off: %q", *got.TracerURL)
	}

	if got.TracerRatio != nil {
		t.Errorf("the sampling ratio survived switching tracing off: %v", *got.TracerRatio)
	}
}

func TestNormaliseTrimsAndAcceptsAnExporterInAnyCase(t *testing.T) {
	got := model.Telemetry{
		TraceExporter: new("  OTLP "),
		TracerURL:     new("  jaeger:4317  "),
		LogLevel:      new(" debug "),
	}.Normalise()

	// Stored the way GoFr will read it, so the screen and the process say the
	// same word.
	if *got.LogLevel != "DEBUG" {
		t.Errorf("expected the level upper-cased and trimmed, got %q", *got.LogLevel)
	}

	if *got.TraceExporter != model.TraceExporterOTLP {
		t.Errorf("expected %q, got %q", model.TraceExporterOTLP, *got.TraceExporter)
	}

	if *got.TracerURL != "jaeger:4317" {
		t.Errorf("expected the address trimmed, got %q", *got.TracerURL)
	}

	if invalid := got.InvalidTelemetryFields(); len(invalid) > 0 {
		t.Errorf("expected this to be accepted, but %v were rejected", invalid)
	}
}

func TestValidationRejectsWhatWouldSilentlyDropEverySpan(t *testing.T) {
	cases := []struct {
		name  string
		given model.Telemetry
		field string
	}{
		{
			"an exporter GoFr does not know hands it a nil exporter, which drops every span",
			model.Telemetry{TraceExporter: new("otel"), TracerURL: new("collector:4317")},
			"traceExporter",
		},
		{
			"zipkin is on its way out upstream and is not offered",
			model.Telemetry{TraceExporter: new("zipkin"), TracerURL: new("zipkin:9411")},
			"traceExporter",
		},
		{
			"the hosted gofr exporter would post every span to a third party",
			model.Telemetry{TraceExporter: new("gofr")},
			"traceExporter",
		},
		{
			"an exporter with no collector leaves tracing off while the screen shows it on",
			model.Telemetry{TraceExporter: new(model.TraceExporterOTLP)},
			"tracerUrl",
		},
		{
			"a scheme is what every collector's own documentation shows, and it resolves nothing",
			model.Telemetry{
				TraceExporter: new(model.TraceExporterOTLP),
				TracerURL:     new("http://jaeger:4317"),
			},
			"tracerUrl",
		},
		{
			"a path is read as part of the host name",
			model.Telemetry{
				TraceExporter: new(model.TraceExporterOTLP),
				TracerURL:     new("jaeger:4317/v1/traces"),
			},
			"tracerUrl",
		},
		{
			"a host with no port cannot be dialled",
			model.Telemetry{
				TraceExporter: new(model.TraceExporterJaeger),
				TracerURL:     new("jaeger"),
			},
			"tracerUrl",
		},
		{
			"a port that is not a number cannot be dialled either",
			model.Telemetry{
				TraceExporter: new(model.TraceExporterJaeger),
				TracerURL:     new("jaeger:grpc"),
			},
			"tracerUrl",
		},
		{
			"a port above the highest there is",
			model.Telemetry{
				TraceExporter: new(model.TraceExporterJaeger),
				TracerURL:     new("jaeger:70000"),
			},
			"tracerUrl",
		},
		{
			"a ratio above one would be clamped silently",
			model.Telemetry{TracerRatio: new(1.5)},
			"tracerRatio",
		},
		{
			"a negative ratio would be clamped to sampling nothing",
			model.Telemetry{TracerRatio: new(-0.1)},
			"tracerRatio",
		},
		{
			// It compares false against every bound, so the obvious check lets it
			// through - and then encoding it back out fails, which would leave the
			// screen that could correct it unable to render at all.
			"a ratio of NaN passes both bounds and then cannot be written back",
			model.Telemetry{TracerRatio: new(math.NaN())},
			"tracerRatio",
		},
		{
			"an infinite ratio cannot be written back either",
			model.Telemetry{TracerRatio: new(math.Inf(1))},
			"tracerRatio",
		},
		{
			// GoFr does not refuse this, it reads it as INFO - so an
			// administrator who asked for more logging would get none and no
			// word about why.
			"a log level GoFr would silently read as INFO",
			model.Telemetry{LogLevel: new("verbose")},
			"logLevel",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invalid := tc.given.Normalise().InvalidTelemetryFields()

			if len(invalid) == 0 {
				t.Fatalf("expected %s to be rejected", tc.field)
			}

			if invalid[0] != tc.field {
				t.Errorf("expected %q to be named, got %v", tc.field, invalid)
			}
		})
	}
}

func TestValidationAcceptsWhatGoFrCanActuallyUse(t *testing.T) {
	cases := map[string]model.Telemetry{
		"a collector by name": {
			TraceExporter: new(model.TraceExporterOTLP),
			TracerURL:     new("jaeger:4317"),
			TracerRatio:   new(0.1),
		},
		"a collector by address": {
			TraceExporter: new(model.TraceExporterJaeger),
			TracerURL:     new("10.0.0.4:4317"),
		},
		"a collector on IPv6": {
			TraceExporter: new(model.TraceExporterOTLP),
			TracerURL:     new("[::1]:4317"),
		},
		// Zero is a real choice: it keeps the exporter configured while recording
		// nothing, which is how sampling is turned down without being torn out.
		"a ratio of zero": {
			TraceExporter: new(model.TraceExporterOTLP),
			TracerURL:     new("jaeger:4317"),
			TracerRatio:   new(0.0),
		},
		"metrics switched off on their own": {
			MetricsOff: true,
		},
		// GoFr reads the level case-insensitively, so this is not a mistake to
		// refuse - Normalise stores it the way it will be read.
		"a log level in any case": {
			LogLevel: new("debug"),
		},
		// Clearing the field on the screen means "follow the file" rather than
		// "store a blank", which GoFr would read as INFO.
		"an empty log level": {
			LogLevel: new(""),
		},
	}

	for name, given := range cases {
		if invalid := given.Normalise().InvalidTelemetryFields(); len(invalid) > 0 {
			t.Errorf("%s: expected this to be accepted, but %v were rejected", name, invalid)
		}
	}
}
