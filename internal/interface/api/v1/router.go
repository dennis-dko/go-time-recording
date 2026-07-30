// Package v1 registers version 1 of the HTTP API.
package v1

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// Handlers bundles the handlers a route registration needs.
type Handlers struct {
	Users      *rest.UserHandler
	Projects   *rest.ProjectHandler
	Timesheets *rest.TimesheetHandler
}

// RegisterRoutes registers all v1 routes.
//
// The routes are declared explicitly rather than with GoFr's AddRESTHandlers:
// that helper generates CRUD straight against a database table by reflecting
// over a struct, which would bypass the domain and application layers this
// project is built around.
func RegisterRoutes(app *gofr.App, h Handlers) {
	const base = "/api/v1"

	app.GET(base+"/users", h.Users.List)
	app.GET(base+"/users/{id}", h.Users.Get)
	app.POST(base+"/users", h.Users.Create)
	app.PUT(base+"/users/{id}", h.Users.Update)
	app.DELETE(base+"/users/{id}", h.Users.Delete)
	app.PUT(base+"/users/{id}/role", h.Users.AssignRole)

	app.GET(base+"/projects", h.Projects.List)
	app.GET(base+"/projects/{id}", h.Projects.Get)
	app.POST(base+"/projects", h.Projects.Create)
	app.PUT(base+"/projects/{id}", h.Projects.Update)
	app.DELETE(base+"/projects/{id}", h.Projects.Delete)
	app.POST(base+"/projects/{id}/archive", h.Projects.Archive)
	app.GET(base+"/projects/{id}/report", h.Timesheets.Report)

	app.GET(base+"/timesheets", h.Timesheets.List)
	app.GET(base+"/timesheets/{id}", h.Timesheets.Get)
	app.POST(base+"/timesheets", h.Timesheets.Create)
	app.PUT(base+"/timesheets/{id}", h.Timesheets.Update)
	app.DELETE(base+"/timesheets/{id}", h.Timesheets.Delete)
	app.POST(base+"/timesheets/{id}/transfer", h.Timesheets.Transfer)
}
