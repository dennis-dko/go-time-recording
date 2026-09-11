package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// TelemetryResponse carries the administered metrics and tracing settings
// together with what this process is actually doing.
//
// Both, because they disagree until the next restart, and a screen that showed
// only the stored values would report tracing as configured while no span had
// been exported since it was saved.
type TelemetryResponse struct {
	// Configured holds only what has been administered; an absent field follows
	// the configuration file.
	Configured model.Telemetry `json:"configured"`

	// Active is what this process is serving and exporting right now.
	Active ActiveTelemetry `json:"active"`

	// RestartRequired is true on a save: GoFr binds the metrics port and builds
	// the trace exporter at start-up, so nothing here can take effect sooner.
	RestartRequired bool `json:"restartRequired"`
}

// ActiveTelemetry is the telemetry in force in this process, on the wire.
type ActiveTelemetry struct {
	LogLevel string `json:"logLevel"`

	MetricsServed bool   `json:"metricsServed"`
	MetricsPort   int    `json:"metricsPort"`
	MetricsPath   string `json:"metricsPath"`

	// TraceExporter is empty when spans go nowhere, which is the default.
	TraceExporter string  `json:"traceExporter"`
	TracerURL     string  `json:"tracerUrl"`
	TracerRatio   float64 `json:"tracerRatio"`
}

func newActiveTelemetry(t appconfig.Telemetry) ActiveTelemetry {
	return ActiveTelemetry{
		LogLevel:      t.LogLevel,
		MetricsServed: t.MetricsServed(),
		MetricsPort:   t.MetricsPort,
		MetricsPath:   appconfig.MetricsPath,
		TraceExporter: t.TraceExporter,
		TracerURL:     t.TracerURL,
		TracerRatio:   t.TracerRatio,
	}
}

// Telemetry handles GET /api/v1/settings/telemetry.
func (h *SettingsHandler) Telemetry(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	configured, err := h.settings.Telemetry(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return TelemetryResponse{
		Configured: configured,
		Active:     h.active(),
	}, nil
}

// SaveTelemetry handles PUT /api/v1/settings/telemetry.
//
// The settings are written to the database and read back out of it before
// gofr.New() on the next start; they are deliberately not applied to the running
// process. Switching the metrics listener or replacing the trace provider under
// live requests would mean reimplementing what GoFr does at start-up and mutating
// a global while it is in use, for a convenience nobody asked for.
func (h *SettingsHandler) SaveTelemetry(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req model.Telemetry
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.settings.SaveTelemetry(c, req); err != nil {
		return nil, toHTTPError(err)
	}

	// Read back rather than echo: the collector address is trimmed on the way in,
	// and an exporter of "off" clears it, so echoing the request would show a
	// setting the next start is not going to use.
	stored, err := h.settings.Telemetry(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// In force from the next line written, rather than from the next start. An
	// empty value means "follow the configuration file", which is a value only
	// main knows - so it is passed through as the empty string and resolved
	// there.
	if h.logLevel != nil {
		level := ""
		if stored.LogLevel != nil {
			level = *stored.LogLevel
		}

		h.logLevel(level)
	}

	return TelemetryResponse{
		Configured: stored,
		Active:     h.active(),

		// The log level is exempt now, so a save that changed only that needs
		// nothing further. Everything else here is still built inside gofr.New().
		RestartRequired: h.logLevel == nil || !onlyLogLevelChanged(stored, h.activeTelemetry),
	}, nil
}

// onlyLogLevelChanged reports whether a save left everything that needs a
// restart exactly as the running process has it.
func onlyLogLevelChanged(stored model.Telemetry, running appconfig.Telemetry) bool {
	if stored.MetricsOff && running.MetricsServed() {
		return false
	}

	if stored.TraceExporter != nil && *stored.TraceExporter != running.TraceExporter {
		return false
	}

	if stored.TracerURL != nil && *stored.TracerURL != running.TracerURL {
		return false
	}

	if stored.TracerRatio != nil && *stored.TracerRatio != running.TracerRatio {
		return false
	}

	return true
}

// active is what this process is doing now, with the log level read live where
// it can be: that one is no longer whatever the process started with.
func (h *SettingsHandler) active() ActiveTelemetry {
	out := newActiveTelemetry(h.activeTelemetry)

	if h.runningLevel != nil {
		if level := h.runningLevel(); level != "" {
			out.LogLevel = level
		}
	}

	return out
}
