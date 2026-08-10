// Package v1 registers version 1 of the HTTP API.
package v1

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// Handlers bundles the handlers a route registration needs.
type Handlers struct {
	Auth       *rest.AuthHandler
	Users      *rest.UserHandler
	Roles      *rest.RoleHandler
	Projects   *rest.ProjectHandler
	Timesheets *rest.TimesheetHandler
	Me         *rest.MeHandler
	Settings   *rest.SettingsHandler
	Tokens     *rest.APITokenHandler
	LDAPSync   *rest.LDAPSyncHandler
	Setup      *rest.SetupHandler
	Passkeys   *rest.PasskeyHandler
	Logs       *rest.LogHandler
	Restart    *rest.RestartHandler
	Timers     *rest.TimerHandler
	Statistics *rest.StatisticsHandler
	Workbook   *rest.WorkbookHandler
	Sheets     *rest.SheetHandler
}

// RegisterRoutes registers all v1 routes.
//
// The routes are declared explicitly rather than with GoFr's AddRESTHandlers:
// that helper generates CRUD straight against a database table by reflecting
// over a struct, which would bypass the domain and application layers this
// project is built around.
//
// Authorization is likewise not attached here but inside each handler, because
// several rules depend on the resource rather than the route: reading your own
// time entries and reading everyone's share one path.
func RegisterRoutes(app *gofr.App, h Handlers) {
	const base = "/api/v1"

	// Sign-in is the one endpoint that must work without a session.
	app.POST(base+"/auth/login", h.Auth.Login)

	// Reachable without a session, because this is how a session begins. The
	// support endpoint tells the interface whether to offer passkeys at all:
	// browsers expose WebAuthn only on HTTPS or localhost.
	app.GET(base+"/auth/passkey", h.Passkeys.Support)
	app.POST(base+"/auth/passkey/login", h.Passkeys.BeginLogin)
	app.PUT(base+"/auth/passkey/login", h.Passkeys.FinishLogin)
	app.POST(base+"/auth/logout", h.Auth.Logout)
	app.GET(base+"/languages", h.Auth.Languages)

	// Branding is readable without a session: the sign-in screen shows the
	// instance's own title and logo before anyone has authenticated.
	app.GET(base+"/branding", h.Settings.Branding)

	// Also readable without a session, so the sign-in screen can say why nothing
	// works rather than letting requests fail without explanation.
	app.GET(base+"/maintenance", h.Settings.Maintenance)
	app.PUT(base+"/settings/maintenance", h.Settings.SaveMaintenance)

	app.PUT(base+"/settings/branding", h.Settings.SaveBranding)
	app.GET(base+"/settings/ldap", h.Settings.LDAP)
	app.PUT(base+"/settings/ldap", h.Settings.SaveLDAP)
	app.POST(base+"/settings/ldap/test", h.Settings.TestLDAP)
	app.POST(base+"/settings/ldap/sync/preview", h.LDAPSync.Preview)
	app.POST(base+"/settings/ldap/sync", h.LDAPSync.Run)
	app.GET(base+"/setup", h.Setup.State)
	app.POST(base+"/setup/complete", h.Setup.Complete)
	app.GET(base+"/settings/operational", h.Settings.Operational)
	app.PUT(base+"/settings/operational", h.Settings.SaveOperational)
	app.GET(base+"/settings/timezone", h.Settings.Timezone)
	app.PUT(base+"/settings/timezone", h.Settings.SaveTimezone)
	app.GET(base+"/settings/datasource", h.Settings.Datasource)
	app.PUT(base+"/settings/datasource", h.Settings.SaveDatasource)
	app.POST(base+"/settings/datasource/test", h.Settings.TestDatasource)
	app.GET(base+"/settings/telemetry", h.Settings.Telemetry)
	app.PUT(base+"/settings/telemetry", h.Settings.SaveTelemetry)

	// What is waiting for a restart, and the restart itself. Under /settings/
	// like the rest, which also keeps it reachable during maintenance mode - the
	// settings that need a restart are exactly the ones somebody is likely to be
	// changing while the installation is out of service.
	app.GET(base+"/settings/restart", h.Restart.State)
	app.POST(base+"/settings/restart", h.Restart.Restart)

	// The process log. Under /admin rather than /settings because it changes
	// nothing - and behind the built-in administrator, because it carries
	// everything the process has written.
	app.GET(base+"/admin/logs", h.Logs.Logs)

	app.GET(base+"/me", h.Me.Me)
	app.PUT(base+"/me/password", h.Me.ChangePassword)
	app.PUT(base+"/me/language", h.Auth.SetLanguage)
	app.PUT(base+"/me/timezone", h.Auth.SetTimezone)
	app.PUT(base+"/me/tour", h.Auth.SetTourSeen)
	// The clock, always the caller's own: starting somebody else's would record
	// time nobody asked them about, so there is no user id in these routes and no
	// permission for "other people's timers" to get wrong.
	// What your own time adds up to, for charting it. Keyed on the caller, so it
	// needs nothing beyond reading your own entries - unlike the project report,
	// which needs reports:read and is the built-in administrator's alone.
	app.GET(base+"/me/statistics", h.Statistics.Own)

	app.GET(base+"/me/timer", h.Timers.Running)
	app.POST(base+"/me/timer", h.Timers.Start)
	app.POST(base+"/me/timer/stop", h.Timers.Stop)
	app.DELETE(base+"/me/timer", h.Timers.Discard)

	app.GET(base+"/me/passkeys", h.Passkeys.List)
	app.POST(base+"/me/passkeys/register", h.Passkeys.BeginRegistration)
	app.PUT(base+"/me/passkeys/register", h.Passkeys.FinishRegistration)
	app.DELETE(base+"/me/passkeys/{id}", h.Passkeys.Delete)
	app.POST(base+"/me/totp", h.Auth.BeginTOTP)
	app.PUT(base+"/me/totp", h.Auth.ConfirmTOTP)
	app.DELETE(base+"/me/totp", h.Auth.DisableTOTP)
	app.GET(base+"/me/tokens", h.Tokens.List)
	app.POST(base+"/me/tokens", h.Tokens.Create)
	app.DELETE(base+"/me/tokens/{id}", h.Tokens.Revoke)
	app.GET(base+"/overtime", h.Me.TeamOvertime)

	// Ahead of the {id} routes for the same reason the timesheet pair below is:
	// "export" is not an id, and this router matches the first pattern that fits.
	app.GET(base+"/users/export", h.Sheets.ExportUsers)
	app.POST(base+"/users/import", h.Sheets.ImportUsers)

	app.GET(base+"/users", h.Users.List)
	app.GET(base+"/users/{id}", h.Users.Get)
	app.POST(base+"/users", h.Users.Create)
	app.PUT(base+"/users/{id}", h.Users.Update)
	app.DELETE(base+"/users/{id}", h.Users.Delete)
	app.PUT(base+"/users/{id}/role", h.Users.AssignRole)
	app.PUT(base+"/users/{id}/working-times", h.Users.UpdateWorkingTimes)
	app.GET(base+"/users/{id}/overtime", h.Me.Overtime)

	app.GET(base+"/roles", h.Roles.List)
	app.GET(base+"/roles/{id}", h.Roles.Get)
	app.POST(base+"/roles", h.Roles.Create)
	app.PUT(base+"/roles/{id}", h.Roles.Update)
	app.DELETE(base+"/roles/{id}", h.Roles.Delete)
	app.GET(base+"/permissions", h.Roles.Permissions)

	app.GET(base+"/projects/export", h.Sheets.ExportProjects)
	app.POST(base+"/projects/import", h.Sheets.ImportProjects)

	app.GET(base+"/projects", h.Projects.List)
	app.GET(base+"/projects/{id}", h.Projects.Get)
	app.POST(base+"/projects", h.Projects.Create)
	app.PUT(base+"/projects/{id}", h.Projects.Update)
	app.DELETE(base+"/projects/{id}", h.Projects.Delete)
	app.POST(base+"/projects/{id}/archive", h.Projects.Archive)
	app.GET(base+"/projects/{id}/report", h.Timesheets.Report)

	// Ahead of the {id} routes, which this router would otherwise match first:
	// "export" is not an id, and the answer was 400 for a parameter nobody sent.
	app.GET(base+"/timesheets/export", h.Workbook.Export)
	app.POST(base+"/timesheets/import", h.Workbook.Import)

	app.GET(base+"/timesheets", h.Timesheets.List)
	app.GET(base+"/timesheets/{id}", h.Timesheets.Get)
	app.POST(base+"/timesheets", h.Timesheets.Create)
	app.PUT(base+"/timesheets/{id}", h.Timesheets.Update)
	app.DELETE(base+"/timesheets/{id}", h.Timesheets.Delete)
	app.POST(base+"/timesheets/{id}/transfer", h.Timesheets.Transfer)

}
