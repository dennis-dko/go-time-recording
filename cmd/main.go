// Command go-time-recording runs the time recording service: a GoFr HTTP API
// plus the embedded web interface, in a single self-contained binary.
package main

import (
	"context"
	"reflect"
	"time"

	// The timezone database, compiled in. Without it time.LoadLocation depends
	// on zoneinfo files being present on the host, which a scratch or distroless
	// container has none of - and the binary is meant to be self-contained. The
	// cost is a few hundred kilobytes; the alternative is every zone silently
	// resolving to UTC and bookings landing on the wrong day.
	_ "time/tzdata"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/directory"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/tlsserver"
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

	tokenRepo := sqldb.NewAPITokenRepository(db, cfg.Dialect)

	auth := appservice.NewAuthService(userRepo, roleRepo)
	apiTokens := appservice.NewAPITokenService(tokenRepo, userRepo, auth)
	settingsService := appservice.NewSettingsService(settingsRepo, roleRepo, cfg.AppName)
	setup := appservice.NewSetupService(settingsService, userRepo, cfg.Dialect)

	// The directory starts unconfigured and is loaded from the settings once
	// the database is reachable, so a broken LDAP entry cannot stop start-up.
	ldapClient := directory.New()

	// The environment's values are the floor: anything the administrator has not
	// overridden from the Settings screen keeps coming from here.
	limits := appservice.NewLimitsProvider(settingsService, model.Limits{
		SessionLifetimeHours:   cfg.SessionLifetime.Hours(),
		MaxDailyHours:          cfg.MaxDailyHours,
		RateLimit:              cfg.RateLimit,
		RateLimitWindowSeconds: int(cfg.RateLimitWindow.Seconds()),
		AutoCloseAfterDays:     cfg.AutoCloseAfterDays,
		LDAPSyncMaxDeleteRatio: cfg.LDAPSyncMaxDeleteRatio,
	})

	sessions := appservice.NewSessionService(userRepo, roleRepo, sessionRepo, auth, cfg.SessionLifetime).
		WithExternalAuth(ldapClient, model.RoleEmployee).
		WithLimits(limits)

	ldapSync := appservice.NewLDAPSyncService(ldapClient, userRepo, roleRepo, timesheetRepo,
		userRepo, cfg.LDAPSyncMaxDeleteRatio, model.RoleEmployee).
		WithLimits(limits)

	users := appservice.NewUserApplicationService(userRepo, roleRepo)
	roles := appservice.NewRoleApplicationService(roleRepo)
	projects := appservice.NewProjectApplicationService(projectRepo, timesheetRepo)
	timesheets := appservice.NewTimesheetApplicationService(
		timesheetRepo, userRepo, projectRepo, cfg.MaxDailyHours).
		WithLimits(limits)
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

	// Order matters, and GoFr applies middleware in registration order:
	// security headers first so they are set even on a rejected request, then
	// the rate limit, then the cookie queue wrapping the two authentication
	// paths, and only then the UI.
	app.UseMiddleware(rest.SecurityHeaders(cfg.HSTSMaxAge))
	app.UseMiddleware(rest.NewRateLimiter(cfg.RateLimit, cfg.RateLimitWindow).
		WithLimits(limits.RateLimit).Middleware())
	// Before the authentication middleware, so a forged request is turned away
	// without a session ever being resolved for it.
	app.UseMiddleware(rest.CSRFMiddleware())
	app.UseMiddleware(rest.CookieMiddleware())
	app.UseMiddleware(rest.SessionMiddleware(sessions))
	// Tokens are checked after sessions and only fill in when no session was
	// found, so a browser session always wins over a stray header.
	app.UseMiddleware(rest.APITokenMiddleware(apiTokens))

	if cfg.UIEnabled {
		// GoFr's AddStaticFiles only serves a directory from disk, which would
		// defeat the single-binary goal, so the embedded UI is installed as
		// middleware instead. The assets stay public: the sign-in form has to
		// be reachable before there is a session.
		app.UseMiddleware(web.Handler())
	}

	// Read per request rather than cached, so changing the instance zone takes
	// effect at once instead of at the next restart.
	instanceTimezone := rest.InstanceTimezoneFunc(func(ctx context.Context) string {
		name, err := settingsService.Timezone(ctx)
		if err != nil {
			return model.DefaultTimezone
		}

		return name
	})

	v1.RegisterRoutes(app, v1.Handlers{
		Auth:       rest.NewAuthHandler(sessions, authorizer, cfg.AppName, instanceTimezone),
		Users:      rest.NewUserHandler(users, userDomain, authorizer, auth, instanceTimezone),
		Roles:      rest.NewRoleHandler(roles, authorizer, auth),
		Projects:   rest.NewProjectHandler(projects, projectDomain, authorizer),
		Timesheets: rest.NewTimesheetHandler(timesheets, timesheetDomain, authorizer, instanceTimezone),
		Me:         rest.NewMeHandler(auth, sessions, overtime, authorizer, instanceTimezone),
		Tokens:     rest.NewAPITokenHandler(apiTokens, authorizer),
		LDAPSync:   rest.NewLDAPSyncHandler(ldapSync, authorizer),
		Setup:      rest.NewSetupHandler(setup, authorizer),
		Settings: rest.NewSettingsHandler(settingsService, authorizer, limits, cfg.Dialect,
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

	// Directory reconciliation. Off unless a schedule is configured, because a
	// run deletes accounts the directory no longer holds together with their
	// recorded hours.
	if cfg.LDAPSyncSchedule != "" {
		app.AddCronJob(cfg.LDAPSyncSchedule, "ldap-sync", func(ctx *gofr.Context) {
			report, err := ldapSync.Sync(ctx)
			if err != nil {
				ctx.Logger.Errorf("directory sync failed: %v", err)

				return
			}

			if report.Aborted != "" {
				ctx.Logger.Warnf("directory sync refused: %s", report.Aborted)

				return
			}

			for _, removed := range report.Deleted {
				ctx.Logger.Warnf("directory sync removed %q (%d time entries)",
					removed.Email, removed.Timesheets)
			}

			if len(report.Created) > 0 {
				ctx.Logger.Infof("directory sync added %d account(s)", len(report.Created))
			}
		})
	}

	// Nightly sweep so stale open entries do not linger unreported.
	if cfg.AutoCloseSchedule != "" {
		app.AddCronJob(cfg.AutoCloseSchedule, "auto-submit-stale-timesheets",
			worker.AutoSubmitStaleTimesheets(timesheetRepo,
				func(ctx *gofr.Context) int { return limits.Limits(ctx).AutoCloseAfterDays },
				func(ctx *gofr.Context) *time.Location {
					// No user in a cron run, so the instance zone is the only one
					// that could apply.
					return model.EffectiveTimezone("", instanceTimezone(ctx))
				}))
	}

	if stop := startTLS(app, cfg); stop != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), tlsShutdownGrace)
			defer cancel()

			if err := stop(ctx); err != nil {
				app.Logger().Errorf("stopping the HTTPS server: %v", err)
			}
		}()
	}

	app.Run()
}

// tlsShutdownGrace bounds how long the HTTPS listener is given to drain.
const tlsShutdownGrace = 10 * time.Second

// startTLS puts a Let's Encrypt terminated HTTPS listener in front of GoFr's
// plain one, and returns its shutdown function.
//
// GoFr owns its listener and accepts only a static certificate pair, so
// automatic certificates are terminated in front of it and proxied to
// localhost. Returns nil when TLS is switched off.
func startTLS(app *gofr.App, cfg appconfig.Config) func(context.Context) error {
	if !cfg.TLSEnabled {
		app.Logger().Warn(
			"TLS_ENABLED is false: traffic is served over plain HTTP. " +
				"Set TLS_ENABLED=true with TLS_DOMAINS to serve HTTPS with Let's Encrypt")

		return nil
	}

	if len(cfg.TLSDomains) == 0 {
		app.Logger().Errorf("TLS_ENABLED is true but TLS_DOMAINS is empty; continuing without HTTPS")

		return nil
	}

	backendPort := app.Config.GetOrDefault("HTTP_PORT", "8000")

	stop, err := tlsserver.Start(tlsserver.Config{
		Domains:   cfg.TLSDomains,
		Email:     cfg.TLSEmail,
		CacheDir:  cfg.TLSCacheDir,
		HTTPSPort: cfg.TLSPort,
		HTTPPort:  cfg.HTTPPort,
		Backend:   "127.0.0.1:" + backendPort,
		Staging:   cfg.TLSStaging,
	}, app.Logger())
	if err != nil {
		app.Logger().Errorf("could not start HTTPS: %v; continuing without it", err)

		return nil
	}

	if cfg.TLSStaging {
		app.Logger().Warn("TLS_STAGING is on: certificates come from Let's Encrypt's test " +
			"authority and browsers will not trust them")
	}

	return stop
}
