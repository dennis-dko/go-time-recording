package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/restart"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// RestartHandler reports what is waiting for a restart, and performs one.
//
// Several settings here are read only while the application starts: the database
// connection, the log level, the metrics port and the trace exporter. Storing
// them from a screen and then asking somebody to find a shell is most of the way
// to not having administered them at all.
//
// What is pending is worked out here rather than in the interface, because it is
// the same comparison the interface would have to make and there is no second
// place it can be right in.
type RestartHandler struct {
	settings *service.SettingsService
	authz    *Authorizer

	// active is what this process actually started with. Stored settings that
	// differ from it are what "pending" means.
	active appconfig.Config

	// startedAt identifies this process to the screen that asked it to restart.
	//
	// Waiting for the application to stop answering and then answer again does
	// not work: replacing the process image takes milliseconds, and a poll that
	// misses that gap reports success without anything having happened. A value
	// that is different afterwards is the only honest signal.
	startedAt time.Time
}

// NewRestartHandler creates the handler.
func NewRestartHandler(
	settings *service.SettingsService,
	authz *Authorizer,
	active appconfig.Config,
) *RestartHandler {
	return &RestartHandler{
		settings:  settings,
		authz:     authz,
		active:    active,
		startedAt: time.Now(),
	}
}

// PendingChange is one setting whose stored value is not the one in force.
type PendingChange struct {
	// Setting is a key the interface translates, not a sentence.
	Setting string `json:"setting"`

	// Running and Stored are shown as they are, so the administrator can see
	// what the restart will actually change.
	Running string `json:"running"`
	Stored  string `json:"stored"`
}

// RestartResponse describes whether a restart is possible and what it would do.
type RestartResponse struct {
	// Supported is false where the process cannot replace itself, in which case
	// Reason says so in a sentence.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`

	// Pending is empty when the running process already matches what is stored.
	Pending []PendingChange `json:"pending"`

	// StartedAt changes when the process does, which is how the screen knows the
	// restart it asked for actually happened.
	StartedAt time.Time `json:"startedAt"`
}

// State handles GET /api/v1/settings/restart.
func (h *RestartHandler) State(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireSystemAdmin(c); err != nil {
		return nil, err
	}

	pending, err := h.pending(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return RestartResponse{
		Supported: restart.Supported(),
		Reason:    restart.Why(),
		Pending:   pending,
		StartedAt: h.startedAt,
	}, nil
}

// pending compares what is stored against what this process is running with.
func (h *RestartHandler) pending(c *gofr.Context) ([]PendingChange, error) {
	telemetry, err := h.settings.Telemetry(c)
	if err != nil {
		return nil, err
	}

	pending := make([]PendingChange, 0)
	running := h.active.Telemetry

	if telemetry.LogLevel != nil && *telemetry.LogLevel != "" &&
		*telemetry.LogLevel != running.LogLevel {
		pending = append(pending, PendingChange{
			Setting: "logLevel", Running: running.LogLevel, Stored: *telemetry.LogLevel,
		})
	}

	// Only one direction is administrable, so only one direction can be pending:
	// the endpoint being switched off while this process still serves it.
	if telemetry.MetricsOff && running.MetricsServed() {
		pending = append(pending, PendingChange{
			Setting: "metrics", Running: "on", Stored: "off",
		})
	}

	if telemetry.TraceExporter != nil && *telemetry.TraceExporter != running.TraceExporter {
		pending = append(pending, PendingChange{
			Setting: "traceExporter",
			Running: running.TraceExporter, Stored: *telemetry.TraceExporter,
		})
	}

	if telemetry.TracerURL != nil && *telemetry.TracerURL != running.TracerURL {
		pending = append(pending, PendingChange{
			Setting: "tracerUrl", Running: running.TracerURL, Stored: *telemetry.TracerURL,
		})
	}

	// The database connection lives in a file rather than the settings table,
	// and only the dialect is worth comparing: a changed host or password is a
	// change to the same connection, and reporting either would mean reading a
	// stored password to compare it.
	if stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok &&
		stored.Dialect != "" && stored.Dialect != h.active.Dialect {
		pending = append(pending, PendingChange{
			Setting: "database", Running: h.active.Dialect, Stored: stored.Dialect,
		})
	}

	return pending, nil
}

// restartGrace is how long the process waits before replacing itself.
//
// Long enough for the answer to reach the browser that asked - the connection is
// gone the instant execve lands - and short enough that the interface's own
// polling has not yet decided the application is unreachable for some other
// reason.
const restartGrace = 500 * time.Millisecond

// Restart handles POST /api/v1/settings/restart.
//
// It answers first and restarts afterwards, because the reply cannot be sent
// once the process image has been replaced. The interface takes the answer as
// "it is going down now" and starts waiting for it to come back.
func (h *RestartHandler) Restart(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireSystemAdmin(c); err != nil {
		return nil, err
	}

	if !restart.Supported() {
		return nil, toHTTPError(apperror.Invalidf("%s", restart.Why()))
	}

	logger := c.Logger

	go func() {
		time.Sleep(restartGrace)

		logger.Warn("restarting on request from the Settings screen")

		// Only returns if it failed, and then the process is still the one that
		// was running - so this is a message rather than an exit: the
		// installation is still up, and whoever pressed the button needs to know
		// their change has not been applied.
		if err := restart.Now(); err != nil {
			logger.Errorf("the restart failed and the application is still running the "+
				"settings it started with: %v", err)
		}
	}()

	return map[string]any{
		"status":  "restarting",
		"message": "The application is restarting.",
	}, nil
}
