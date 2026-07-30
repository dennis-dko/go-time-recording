// Command go-time-recording runs the time recording service: a GoFr HTTP API
// plus the embedded web interface, in a single self-contained binary.
package main

import (
	"gofr.dev/pkg/gofr"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	v1 "github.com/dennis-dko/go-time-recording/internal/interface/api/v1"
	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/interface/worker"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	app := gofr.New()

	cfg := appconfig.Load(app.Config)
	app.Logger().Infof("go-time-recording %s starting (dialect=%s)", version, cfg.Dialect)

	// Schema first: the binary provisions its own database on first start, so
	// a deployment needs no separate migration step.
	app.Migrate(migrations.All(cfg.Dialect))

	db := app.GetSQL()

	userRepo := sqldb.NewUserRepository(db, cfg.Dialect)
	projectRepo := sqldb.NewProjectRepository(db, cfg.Dialect)
	timesheetRepo := sqldb.NewTimesheetRepository(db, cfg.Dialect)

	users := appservice.NewUserApplicationService(userRepo)
	projects := appservice.NewProjectApplicationService(projectRepo, timesheetRepo)
	timesheets := appservice.NewTimesheetApplicationService(
		timesheetRepo, userRepo, projectRepo, cfg.MaxDailyHours)

	userDomain := domainservice.NewUserDomainService(userRepo)
	projectDomain := domainservice.NewProjectDomainService(projectRepo, timesheetRepo)
	timesheetDomain := domainservice.NewTimesheetDomainService(timesheetRepo, projectRepo, userRepo)

	// Basic auth is opt-in: with no credentials configured the binary starts
	// open, which is what a local trial needs. Registering it before the
	// routes means it covers the UI as well as the API.
	if cfg.AuthEnabled() {
		app.EnableBasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPassword)
		app.Logger().Infof("basic auth enabled for user %q", cfg.BasicAuthUser)
	} else {
		app.Logger().Warn("basic auth disabled: set BASIC_AUTH_USER and BASIC_AUTH_PASSWORD to protect this instance")
	}

	if cfg.UIEnabled {
		// GoFr's AddStaticFiles only serves a directory from disk, which would
		// defeat the single-binary goal, so the embedded UI is installed as
		// middleware instead.
		app.UseMiddleware(web.Handler())
	}

	v1.RegisterRoutes(app, v1.Handlers{
		Users:      rest.NewUserHandler(users, userDomain),
		Projects:   rest.NewProjectHandler(projects, projectDomain),
		Timesheets: rest.NewTimesheetHandler(timesheets, timesheetDomain),
	})

	// Nightly sweep so stale open entries do not linger unreported.
	if cfg.AutoCloseSchedule != "" {
		app.AddCronJob(cfg.AutoCloseSchedule, "auto-submit-stale-timesheets",
			worker.AutoSubmitStaleTimesheets(timesheetRepo, cfg.AutoCloseAfterDays))
	}

	app.Run()
}
