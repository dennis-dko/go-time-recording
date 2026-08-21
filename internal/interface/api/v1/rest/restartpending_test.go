package rest

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// waitingFor reports whether one setting is in the list.
func waitingFor(changes []PendingChange, setting string) (PendingChange, bool) {
	for _, change := range changes {
		if change.Setting == setting {
			return change, true
		}
	}

	return PendingChange{}, false
}

func ptr[T any](v T) *T { return &v }

// What a restart would change is what the next start would use, against what
// this process is using.
//
// The comparison used to be "a stored value that differs from the running one",
// which is silent about the change that brought this to light: clearing a field
// back to "follow the configuration file". An absent stored value was read as
// nothing to say, when it means "use the file's value" - and the file's value is
// frequently not what is running, because what is running came from the stored
// one.
//
// Reported by somebody switching the trace exporter to OTLP, being told to
// restart, switching it back, and being told nothing.
func TestARestartIsReportedWhenTheNextStartWouldDiffer(t *testing.T) {
	// A process that is exporting because a stored setting told it to, and a
	// configuration file that says nothing about exporting.
	running := appconfig.Telemetry{
		LogLevel: "INFO", TraceExporter: "otlp",
		TracerURL: "collector:4317", TracerRatio: 1, MetricsPort: 2121,
	}

	fromFile := appconfig.Telemetry{
		LogLevel: "INFO", TracerRatio: 1, MetricsPort: 2121,
	}

	t.Run("cleared back to the file", func(t *testing.T) {
		// Nothing stored: every field follows the configuration file again.
		changes := telemetryPending(model.Telemetry{}, fromFile, running, true)

		for _, setting := range []string{"traceExporter", "tracerUrl"} {
			change, found := waitingFor(changes, setting)
			if !found {
				t.Errorf("%q was cleared while this process is using it, and is not "+
					"reported as waiting: %+v", setting, changes)

				continue
			}

			if change.Stored != "" {
				t.Errorf("%q says it will become %q; the file sets nothing, so it "+
					"becomes empty", setting, change.Stored)
			}
		}
	})

	t.Run("stored and the same as running", func(t *testing.T) {
		changes := telemetryPending(model.Telemetry{
			TraceExporter: ptr("otlp"),
			TracerURL:     ptr("collector:4317"),
			TracerRatio:   ptr(1.0),
		}, fromFile, running, true)

		if len(changes) != 0 {
			t.Errorf("nothing was changed and %d setting(s) are reported as "+
				"waiting: %+v", len(changes), changes)
		}
	})

	t.Run("stored and different", func(t *testing.T) {
		changes := telemetryPending(model.Telemetry{
			TracerRatio: ptr(0.25),
		}, fromFile, running, true)

		change, found := waitingFor(changes, "tracerRatio")
		if !found {
			t.Fatalf("a changed sampling share is not reported: %+v", changes)
		}

		if change.Stored != "0.25" {
			t.Errorf("the share says it will become %q, want %q", change.Stored, "0.25")
		}
	})
}

// The metrics endpoint is administrable in both directions, so both can be
// waiting.
//
// Only the off direction was compared, on the stated grounds that only one is
// administrable. The form sends metricsOff when the box is ticked and leaves it
// out when it is not, so clearing it is how the endpoint is switched back on -
// and the port is bound while the process starts, so it stayed dark with the
// card saying nothing.
func TestMetricsAreWaitingInBothDirections(t *testing.T) {
	serving := appconfig.Telemetry{LogLevel: "INFO", MetricsPort: 2121, TracerRatio: 1}
	dark := appconfig.Telemetry{LogLevel: "INFO", MetricsPort: 0, TracerRatio: 1}

	t.Run("switched off while this process serves them", func(t *testing.T) {
		changes := telemetryPending(model.Telemetry{MetricsOff: true}, serving, serving, true)

		change, found := waitingFor(changes, "metrics")
		if !found {
			t.Fatalf("switching metrics off is not reported: %+v", changes)
		}

		if change.Running != "on" || change.Stored != "off" {
			t.Errorf("the card says %q → %q, want on → off", change.Running, change.Stored)
		}
	})

	t.Run("switched back on while this process does not", func(t *testing.T) {
		// The file serves them; a stored "off" is what made this process dark;
		// clearing that is how they come back.
		changes := telemetryPending(model.Telemetry{}, serving, dark, true)

		change, found := waitingFor(changes, "metrics")
		if !found {
			t.Fatalf("switching metrics back on is not reported: %+v", changes)
		}

		if change.Running != "off" || change.Stored != "on" {
			t.Errorf("the card says %q → %q, want off → on", change.Running, change.Stored)
		}
	})

	// And the case that made the comparison read the file rather than the
	// absence: an installation whose configuration file switches metrics off,
	// with nothing stored. MetricsOff is a bool, so "nobody stored anything"
	// looks exactly like "stored as on" - and reading it that way would promise a
	// restart that switches on an endpoint the file says to leave alone.
	t.Run("off in the file, and nobody has stored anything", func(t *testing.T) {
		changes := telemetryPending(model.Telemetry{}, dark, dark, true)

		if change, found := waitingFor(changes, "metrics"); found {
			t.Errorf("the card promises metrics %q → %q on an installation whose "+
				"configuration file switches them off and where nothing is stored",
				change.Running, change.Stored)
		}
	})
}
