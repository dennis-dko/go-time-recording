// Command go-time-recording runs the time recording service: a GoFr HTTP API
// plus the embedded web interface, in a single self-contained binary.
package main

import (
	"reflect"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/directory"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	v1 "github.com/dennis-dko/go-time-recording/internal/interface/api/v1"
	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/interface/worker"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

// unavailable reports whether the container has no usable SQL datasource.
//
// When GoFr cannot build one it stores a typed nil pointer in the interface,
// so the interface itself is non-nil and a plain `db == nil` misses the case.
func unavailable(db container.DB) bool {
	if db == nil {
		return true
	}

	v := reflect.ValueOf(db)

	return v.Kind() == reflect.Pointer && v.IsNil()
}

func main() {
	// An administered database connection is exported into the environment
	// before GoFr reads its configuration: GoFr lets real environment
	// variables win over its .env files, so this overrides them without
	// touching GoFr's API. It has to happen before gofr.New().
	if ds, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok {
		if err := appconfig.ApplyDatasource(ds); err != nil {
			panic("cannot apply the configured datasource: " + err.Error())
		}
	}

	app := gofr.New()

	cfg := appconfig.Load(app.Config)
	app.Logger().Infof("go-time-recording %s starting (dialect=%s)", version, cfg.Dialect)

	// Schema first: the binary provisions its own database on first start, so
	// a deployment needs no separate migration step.
	app.Migrate(migrations.All(cfg.Dialect))

	db := app.GetSQL()
	if unavailable(db) {
		// Without this the app starts happily and every request panics on a
		// nil datasource, which surfaces as a 500 and a stack trace instead of
		// the actual problem. Refuse to start and say what to check.
		app.Logger().Fatalf(
			"no database connection. GoFr reads ./configs relative to the working "+
				"directory - start the binary from the directory holding configs/, "+
				"and check DB_DIALECT (%q) and the DB_* settings", cfg.Dialect)
	}

	userRepo := sqldb.NewUserRepository(db, cfg.Dialect)
	roleRepo := sqldb.NewRoleRepository(db, cfg.Dialect)
	sessionRepo := sqldb.NewSessionRepository(db, cfg.Dialect)
	projectRepo := sqldb.NewProjectRepository(db, cfg.Dialect)
	timesheetRepo := sqldb.NewTimesheetRepository(db, cfg.Dialect)
	settingsRepo := sqldb.NewSettingsRepository(db, cfg.Dialect)

	auth := appservice.NewAuthService(userRepo, roleRepo)
	settingsService := appservice.NewSettingsService(settingsRepo, roleRepo)

	// The directory starts unconfigured and is loaded from the settings once
	// the database is reachable, so a broken LDAP entry cannot stop start-up.
	ldapClient := directory.New()

	sessions := appservice.NewSessionService(userRepo, roleRepo, sessionRepo, auth, cfg.SessionLifetime).
		WithExternalAuth(ldapClient, model.RoleEmployee)

	users := appservice.NewUserApplicationService(userRepo, roleRepo)
	roles := appservice.NewRoleApplicationService(roleRepo)
	projects := appservice.NewProjectApplicationService(projectRepo, timesheetRepo)
	timesheets := appservice.NewTimesheetApplicationService(
		timesheetRepo, userRepo, projectRepo, cfg.MaxDailyHours)
	overtime := appservice.NewOvertimeService(timesheetRepo, userRepo)

	userDomain := domainservice.NewUserDomainService(userRepo, roleRepo)
	projectDomain := domainservice.NewProjectDomainService(projectRepo, timesheetRepo)
	timesheetDomain := domainservice.NewTimesheetDomainService(timesheetRepo, projectRepo, userRepo)

	// The built-in administrator is created after the migrations have run, so
	// there is always a way in even on a brand new database.
	app.OnStart(func(ctx *gofr.Context) error {
		created, err := auth.EnsureSystemUser(ctx)
		if err != nil {
			return err
		}

		if created {
			ctx.Logger.Warnf(
				"created the built-in administrator %q with the initial password %q - change it on first sign-in",
				appservice.SystemUserEmail, appservice.SystemUserPassword)
		}

		// Load the directory settings now that the database is up. A failure
		// here only means no LDAP; it must not stop the application.
		ldapConfig, err := settingsService.LDAP(ctx)
		if err != nil {
			ctx.Logger.Errorf("could not read the LDAP settings: %v", err)

			return nil
		}

		ldapClient.Configure(ldapConfig)

		if ldapConfig.Enabled {
			ctx.Logger.Infof("LDAP authentication enabled against %s:%d", ldapConfig.Host, ldapConfig.Port)
		}

		return nil
	})

	if cfg.AuthEnabled() {
		app.Logger().Infof("authentication enabled; sign in at / with an account's email address")
	} else {
		app.Logger().Warn(
			"AUTH_ENABLED is false: every request has full administrative access. " +
				"Set AUTH_ENABLED=true to enforce sign-in and role permissions")
	}

	authorizer := rest.NewAuthorizer(auth, cfg.AuthEnabled())

	// Order matters: the cookie queue must wrap the session lookup, which must
	// in turn run before the UI so a signed-in user is known by the time a
	// handler asks. GoFr applies middleware in registration order.
	app.UseMiddleware(rest.CookieMiddleware())
	app.UseMiddleware(rest.SessionMiddleware(sessions))

	if cfg.UIEnabled {
		// GoFr's AddStaticFiles only serves a directory from disk, which would
		// defeat the single-binary goal, so the embedded UI is installed as
		// middleware instead. The assets stay public: the sign-in form has to
		// be reachable before there is a session.
		app.UseMiddleware(web.Handler())
	}

	v1.RegisterRoutes(app, v1.Handlers{
		Auth:       rest.NewAuthHandler(sessions, authorizer, cfg.AppName),
		Users:      rest.NewUserHandler(users, userDomain, authorizer, auth),
		Roles:      rest.NewRoleHandler(roles, authorizer, auth),
		Projects:   rest.NewProjectHandler(projects, projectDomain, authorizer),
		Timesheets: rest.NewTimesheetHandler(timesheets, timesheetDomain, authorizer),
		Me:         rest.NewMeHandler(auth, sessions, overtime, authorizer),
		Settings: rest.NewSettingsHandler(settingsService, authorizer, cfg.Dialect,
			ldapClient.Configure,
			func(ctx *gofr.Context, config model.LDAPConfig) error {
				return ldapClient.TestConnection(ctx, config)
			}),
	})

	// Expired sessions would otherwise accumulate forever.
	app.AddCronJob("0 3 * * *", "prune-expired-sessions", func(ctx *gofr.Context) {
		removed, err := sessions.PruneExpired(ctx)
		if err != nil {
			ctx.Logger.Errorf("pruning sessions: %v", err)

			return
		}

		if removed > 0 {
			ctx.Logger.Infof("pruned %d expired session(s)", removed)
		}
	})

	// Nightly sweep so stale open entries do not linger unreported.
	if cfg.AutoCloseSchedule != "" {
		app.AddCronJob(cfg.AutoCloseSchedule, "auto-submit-stale-timesheets",
			worker.AutoSubmitStaleTimesheets(timesheetRepo, cfg.AutoCloseAfterDays))
	}

	app.Run()
}
