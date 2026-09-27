package rest

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// A save says a restart is needed exactly when the restart card says so.
//
// Two answers to one question. The card asks telemetryPending, which was
// corrected twice - to compare the metrics in both directions, and to read a
// setting cleared back to the configuration file as the change it is. The
// save's own answer made its own comparison and still had both of the old
// mistakes, so switching the metrics back on, or clearing an exporter a stored
// setting had switched on, came back "no restart needed" to anybody reading the
// response, while the process went on doing the old thing.
func TestASaveNeedsARestartExactlyWhenTheCardSaysSo(t *testing.T) {
	fromFile := appconfig.Telemetry{LogLevel: "INFO", TracerRatio: 1, MetricsPort: 2121}

	for name, c := range map[string]struct {
		running appconfig.Telemetry
		stored  model.Telemetry
	}{
		"the metrics switched back on": {
			running: appconfig.Telemetry{LogLevel: "INFO", TracerRatio: 1},
		},
		"an exporter cleared back to the file": {
			running: appconfig.Telemetry{
				LogLevel: "INFO", TracerRatio: 1, MetricsPort: 2121,
				TraceExporter: "otlp", TracerURL: "collector:4317",
			},
		},
		"nothing but the log level": {
			running: fromFile,
			stored:  model.Telemetry{LogLevel: new("DEBUG")},
		},
	} {
		h := (&SettingsHandler{activeTelemetry: c.running, logLevel: func(string) {}}).
			WithFileTelemetry(fromFile)

		card := len(telemetryPending(c.stored, fromFile, c.running, true)) > 0

		if got := h.aSaveNeedsARestart(c.stored); got != card {
			t.Errorf("%s: the save answers restartRequired=%v while the restart card "+
				"says %v", name, got, card)
		}
	}
}
