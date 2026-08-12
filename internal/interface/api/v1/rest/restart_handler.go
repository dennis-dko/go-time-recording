package rest

import (
	"fmt"
	"strings"
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

	// running is the connection this process opened, which is not the same thing
	// as the file that is on disk now - the file is what the settings screen
	// writes, and the point of the comparison is the difference between them.
	//
	// Kept as its own field rather than read from active, which carries only the
	// dialect: everything else about the connection is exported into GoFr's
	// environment and never comes back.
	running appconfig.Datasource

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
	running appconfig.Datasource,
) *RestartHandler {
	return &RestartHandler{
		settings:  settings,
		authz:     authz,
		active:    active,
		running:   running,
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
	// Reason says so in a sentence and ReasonCode names which refusal it is.
	//
	// Both, because they are for different readers. Reason is English prose written
	// where the limitation is decided, which is what a log wants and what a client
	// with no translation for the code falls back to. ReasonCode is what lets the
	// interface say the same thing in the reader's own language - and there is more
	// than one refusal, so a single sentence for all of them would tell somebody on
	// Linux that they are on Windows.
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason,omitempty"`
	ReasonCode string `json:"reasonCode,omitempty"`

	// Pending is empty when the running process already matches what is stored.
	Pending []PendingChange `json:"pending"`

	// StartedAt changes when the process does, which is how the screen knows the
	// restart it asked for actually happened.
	StartedAt time.Time `json:"startedAt"`
}

// State handles GET /api/v1/settings/restart.
func (h *RestartHandler) State(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	pending, err := h.pending(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return RestartResponse{
		Supported:  restart.Supported(),
		Reason:     restart.Why(),
		ReasonCode: restart.Code(),
		Pending:    pending,
		StartedAt:  h.startedAt,
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

	// The directory schedule, which is stored with the rest of the LDAP settings
	// but is the one of them that cannot be applied at once: a cron job is
	// registered while the application starts.
	directory, err := h.settings.LDAP(c)
	if err != nil {
		return nil, err
	}

	if directory.SyncSchedule != h.active.LDAPSyncSchedule {
		pending = append(pending, PendingChange{
			Setting: "directorySchedule",
			Running: h.active.LDAPSyncSchedule, Stored: directory.SyncSchedule,
		})
	}

	// The database connection lives in a file rather than the settings table, and
	// is compared whole.
	//
	// Only the dialect used to be compared, on the grounds that a changed host or
	// password is a change to the same connection. That is a fair description of
	// what the card says and a wrong answer to what it is for: the connection is
	// opened once, while the application starts, so moving the database to another
	// host is exactly as pending as moving it to another dialect. And the save on
	// that screen now promises a restart only when this list is not empty, so a
	// comparison that misses a change is the interface telling somebody there is
	// nothing left to do.
	stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile)
	if !ok || stored.Dialect == "" {
		return pending, nil
	}

	if running, saved := connectionSummary(h.running), connectionSummary(stored); running != saved {
		pending = append(pending, PendingChange{
			Setting: "database", Running: running, Stored: saved,
		})
	}

	// The password is compared and never shown, on either side. What is pending is
	// that it changed; printing the old one next to the new one on a screen would
	// be a worse answer than the empty one the interface renders for this.
	if stored.Password != h.running.Password {
		pending = append(pending, PendingChange{Setting: "databasePassword"})
	}

	return pending, nil
}

// connectionSummary describes a connection in one line, without its password.
//
// The card shows what a restart would change, so the description has to carry
// everything that is compared against it - a comparison that includes the port
// and a description that does not would put "postgres → postgres" on screen and
// call it an explanation.
//
// The port is defaulted through the same function that fills it in for GoFr, so
// a connection that leaves it empty and one that spells out the default it would
// have been given are the same connection here as well as in fact.
func connectionSummary(ds appconfig.Datasource) string {
	dialect := strings.ToLower(ds.Dialect)

	// A file on disk: host, port and user say nothing about it, and the name is
	// the path.
	if dialect != "postgres" && dialect != "mysql" {
		return strings.TrimSpace(dialect + " " + ds.Name)
	}

	summary := fmt.Sprintf("%s %s:%s/%s",
		dialect, ds.Host, appconfig.DefaultPortFor(ds.Dialect, ds.Port), ds.Name)

	if ds.User != "" {
		summary += " as " + ds.User
	}

	if ds.SSLMode != "" {
		summary += " (" + ds.SSLMode + ")"
	}

	return summary
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
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	if !restart.Supported() {
		// One code rather than one per reason. Which reason it is belongs on the
		// card above the button, which already says it in the reader's language and
		// at length; what a refused press has to say is only that it was refused,
		// and it used to say that in English on an otherwise German screen.
		return nil, toHTTPError(apperror.Invalidf("%s", restart.Why()).
			WithCode("restartUnsupported"))
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
