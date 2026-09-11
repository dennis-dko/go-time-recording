package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// SettingsHandler serves the administration screen: branding, the database
// connection and the directory.
type SettingsHandler struct {
	settings *service.SettingsService
	authz    *Authorizer
	ldap     *ldapAdmin

	// limits is dropped from cache after a save, so an administrator sees the
	// change take effect immediately rather than after the refresh interval.
	limits *service.LimitsProvider

	// activeDialect is what this process actually connected to, which differs
	// from the stored settings until the next restart.
	activeDialect string

	// announcements is how every open browser is told the installation has gone
	// out of service. Nil where nothing subscribes, which is every test that
	// does not ask about it.
	announcements *announce.Hub

	// running is the whole connection this process opened, not just its dialect.
	// It is what the datasource screen shows when nothing has been stored, so an
	// installation configured through the environment can see what it is
	// connected to rather than an empty form.
	running appconfig.Datasource

	// activeTelemetry is the metrics and tracing configuration this process
	// started with, for the same reason: what is stored takes effect at the next
	// start, and only this says what is happening now.
	activeTelemetry appconfig.Telemetry

	// version is the build this process was compiled from, reported alongside
	// the branding because the footer renders both and one request is better
	// than two for something on every page.
	version string

	// maintenance is dropped from cache after a save, so the switch takes effect
	// on the next request rather than within the cache interval.
	maintenance MaintenanceState

	// logLevel applies a saved log level to the running process and reports what
	// is in force. The one telemetry setting that does not wait for a restart:
	// the log sink decides what is emitted, so changing it is a store in one
	// place rather than a change to the framework's logger, which is read from
	// every request goroutine without synchronisation.
	//
	// Both nil where the process output is not captured. There is nothing
	// between the framework and the console to apply a level there, so the
	// setting keeps needing a restart and the screen keeps saying so.
	logLevel     func(string)
	runningLevel func() string
}

// WithLiveLogLevel lets a saved log level take effect without a restart.
//
// Fluent rather than a constructor parameter for the same reason
// WithMaintenance is: the sink exists before the handlers and the resolution of
// "follow the configuration file" belongs to main, which knows what the file
// said before the level was widened to capture everything.
func (h *SettingsHandler) WithLiveLogLevel(apply func(string), running func() string) *SettingsHandler {
	h.logLevel, h.runningLevel = apply, running

	return h
}

// WithAnnouncements lets the handler tell every open browser that the
// installation has gone out of service, or come back into it.
func (h *SettingsHandler) WithAnnouncements(hub *announce.Hub) *SettingsHandler {
	h.announcements = hub

	return h
}

// WithMaintenance lets the handler clear the cached maintenance state.
//
// Fluent rather than a constructor parameter because the middleware that owns the
// cache is built after the handlers, and threading it back through the
// constructor would mean building one of them twice.
func (h *SettingsHandler) WithMaintenance(state MaintenanceState) *SettingsHandler {
	h.maintenance = state

	return h
}

// NewSettingsHandler creates the handler.
func NewSettingsHandler(
	settings *service.SettingsService,
	authz *Authorizer,
	limits *service.LimitsProvider,
	activeDialect string,
	activeTelemetry appconfig.Telemetry,
	version string,
	configure func(model.LDAPConfig),
	test func(*gofr.Context, model.LDAPConfig) error,
) *SettingsHandler {
	return &SettingsHandler{
		settings:        settings,
		authz:           authz,
		limits:          limits,
		activeDialect:   activeDialect,
		activeTelemetry: activeTelemetry,
		version:         version,
		ldap:            &ldapAdmin{configure: configure, test: test},
	}
}

// WithRunningConnection attaches the connection this process actually opened.
//
// The screen showed an empty form on any installation configured through the
// environment, because it is filled from the file the installer or the screen
// itself writes and a compose deployment has no such file. Above that empty form
// sat "connected via postgres", which is the running dialect - so the screen said
// it was connected and showed nothing it was connected to.
//
// Worse than looking unconfigured: the file wins over the environment, so
// somebody filling in that form would override the deployment's own settings at
// the next start, with nothing on screen saying so.
func (h *SettingsHandler) WithRunningConnection(running appconfig.Datasource) *SettingsHandler {
	h.running = running

	return h
}

// requireAdmin restricts the screen to the built-in administrator: these
// settings decide where the data lives and who may sign in at all.
func (h *SettingsHandler) requireAdmin(c *gofr.Context) error {
	_, err := h.authz.RequireInstallationAdmin(c)

	return err
}
