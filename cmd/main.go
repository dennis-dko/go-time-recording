// Command go-time-recording runs the time recording service: a GoFr HTTP API
// plus the embedded web interface, in a single self-contained binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	// The timezone database, compiled in. Without it time.LoadLocation depends
	// on zoneinfo files being present on the host, which a scratch or distroless
	// container has none of - and the binary is meant to be self-contained. The
	// cost is a few hundred kilobytes; the alternative is every zone silently
	// resolving to UTC and bookings landing on the wrong day.
	_ "time/tzdata"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/directory"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/logsink"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/tlsserver"
	v1 "github.com/dennis-dko/go-time-recording/internal/interface/api/v1"
	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
	"github.com/dennis-dko/go-time-recording/internal/interface/installer"
	"github.com/dennis-dko/go-time-recording/internal/interface/web"
	"github.com/dennis-dko/go-time-recording/internal/interface/worker"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

// terminal records whether the process output is a console, decided during
// package initialisation - which is before main() replaces os.Stdout to capture
// the log, and therefore the last moment the answer is still the true one.
var terminal = isCharDevice(os.Stdout)

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// runInstaller serves the first-run screen until a database has been chosen.
//
// It reads its own configuration rather than GoFr's, because GoFr has not been
// constructed yet - and cannot be, since constructing it is what needs the
// database. Only the port and the instance name are wanted, and both have a
// sensible answer without a database behind them.
func runInstaller() (appconfig.Datasource, error) {
	settings := appconfig.InstallerSettings()

	// A signal ends the wait, so a container that is stopped while sitting on
	// the installer exits instead of being killed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := logging.NewLogger(logging.INFO)

	return installer.Serve(ctx, installer.Config{
		Addr:           ":" + settings.HTTPPort,
		AppName:        settings.AppName,
		Version:        version,
		Token:          settings.SetupToken,
		DatasourceFile: appconfig.DatasourceFile,
		Prefill:        settings.Prefill,
		Logf:           logger.Infof,
	})
}

// consoleLine renders a captured record for a human watching a terminal.
//
// Not an attempt to reproduce GoFr's own pretty printer, which would drift from
// it at the first release. It shows what is worth seeing while developing: the
// time, the level, the message, and the trace id when a request produced the
// line.
func consoleLine(r logsink.Record) string {
	line := fmt.Sprintf("%s %-6s %s", r.Time.Format("15:04:05"), r.Level, r.Message)

	if r.TraceID != "" {
		line += "  trace=" + r.TraceID
	}

	return line
}

// die reports why the process cannot continue, and stops it.
//
// restore undoes the log capture first, so the message goes to the real stderr
// rather than into a pipe whose reader will not outlive this call. Pass nil when
// nothing was captured.
func die(restore func(), format string, args ...any) {
	if restore != nil {
		restore()
	}

	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// hostSuffix names the server in a failure message, or nothing for SQLite,
// which has none.
func hostSuffix(ds appconfig.Datasource) string {
	if ds.Host == "" {
		return ""
	}

	where := ds.Host
	if ds.Port != "" {
		where += ":" + ds.Port
	}

	return fmt.Sprintf(", host %s, user %q", where, ds.User)
}

// tuneSQLite puts a SQLite database into write-ahead logging.
//
// SQLite's default journal makes a reader and a writer exclude each other, and
// without a busy timeout the loser is refused rather than made to wait. The
// symptom is a request failing with "database is locked (5) (SQLITE_BUSY)"
// whenever a save happens while another page is loading - a 500 for one person
// because somebody else was reading, and worse, a session lookup that fails the
// same way and reads as "not signed in".
//
// This was found by a browser test: the setup wizard's last step failed because
// the administration screen was loading beside it. Two ordinary users on the
// same installation would collide the same way.
//
// WAL lets readers and the writer proceed at once, and it persists in the
// database file rather than in the connection, so setting it once is enough.
// Concurrent *writers* still serialise, and GoFr builds the connection string
// itself - "file:name.db", with nowhere to add a busy timeout - so that narrower
// window remains. It is the right trade for an installation small enough to be
// on SQLite; anything busier belongs on PostgreSQL, which the installer offers.
func tuneSQLite(app *gofr.App, db container.DB, dialect string) {
	if dialect != "sqlite" || unavailable(db) {
		return
	}

	// A pragma answers with a row, so it is a query rather than an exec.
	var mode string

	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		app.Logger().Warnf("could not switch SQLite to write-ahead logging: %v; "+
			"concurrent requests may fail with SQLITE_BUSY", err)

		return
	}

	if !strings.EqualFold(mode, "wal") {
		app.Logger().Warnf("SQLite stayed in %q journal mode; concurrent requests may fail "+
			"with SQLITE_BUSY", mode)
	}
}

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
	// Capture the process output before anything writes to it. GoFr's logger
	// takes os.Stdout when gofr.New() constructs it and keeps it for the life
	// of the process, so this is the only moment at which the log viewer can be
	// given something to show. See the logsink package for what that costs.
	logs := logsink.New(logsink.DefaultCapacity)

	if terminal {
		// Interception makes the output a pipe, and GoFr prints JSON when its
		// output is not a terminal. On a terminal that would be a regression in
		// readability for no gain, so the captured records are rendered back to
		// human lines on the way to the console.
		logs.SetPassthroughRenderer(consoleLine)
	}

	restoreOutput, err := logs.Capture()
	if err != nil {
		// Not fatal: an application that refuses to start because it could not
		// install a log viewer has its priorities wrong.
		_, _ = os.Stderr.WriteString("could not capture the log for the viewer: " + err.Error() + "\n")
	} else {
		defer restoreOutput()
	}

	// An administered database connection is exported into the environment
	// before GoFr reads its configuration: GoFr lets real environment
	// variables win over its .env files, so this overrides them without
	// touching GoFr's API. It has to happen before gofr.New().
	// Three places a connection can come from, in order of precedence: the file
	// the installer or the settings screen wrote, then the environment, then a
	// person answering the installer.
	ds, configured := appconfig.LoadDatasource(appconfig.DatasourceFile)
	if !configured {
		ds, configured = appconfig.DatasourceFromEnvironment()
	}

	// Nothing anywhere: serve the installer until somebody chooses a database,
	// then carry on into the application in this same process. See the installer
	// package for why this cannot be a step of the in-application wizard.
	if !configured {
		chosen, err := runInstaller()
		if err != nil {
			die(restoreOutput, "cannot run the installer: %v", err)
		}

		ds = chosen
	}

	// Proven before GoFr touches it, with the same drivers GoFr will use. GoFr
	// discovers an unreachable database part-way through its migrations and
	// exits on a message about a table it could not create, which describes
	// neither what is wrong nor where.
	if err := appconfig.TestDatasource(context.Background(), ds); err != nil {
		die(restoreOutput,
			"cannot reach the configured database.\n"+
				"  %v\n"+
				"  dialect %q, name %q%s\n"+
				"  Remove DB_DIALECT and %s to choose a connection interactively instead.",
			err, ds.Dialect, ds.Name, hostSuffix(ds), appconfig.DatasourceFile)
	}

	if err := appconfig.ApplyDatasource(ds); err != nil {
		die(restoreOutput, "cannot apply the configured datasource: %v", err)
	}

	app := gofr.New()

	cfg := appconfig.Load(app.Config)
	app.Logger().Infof("go-time-recording %s starting (dialect=%s)", version, cfg.Dialect)

	db := app.GetSQL()

	// Before the migrations, because they are the first writes.
	tuneSQLite(app, db, cfg.Dialect)

	// Schema first: the binary provisions its own database on first start, so
	// a deployment needs no separate migration step.
	app.Migrate(migrations.All(cfg.Dialect))

	if unavailable(db) {
		// Without this the app starts happily and every request panics on a
		// nil datasource, which surfaces as a 500 and a stack trace instead of
		// the actual problem. Refuse to start and say what to check.
		//
		// Written straight to stderr rather than logged, and only after the
		// capture is undone: a Fatal goes through the pipe the log viewer reads
		// from, and the process exits before that reader is scheduled - so the
		// one message an operator actually needs is the one that would go
		// missing. See the logsink package.
		die(restoreOutput,
			"no database connection.\n"+
				"  configured dialect: %q\n"+
				"  GoFr reads ./configs relative to the working directory, so start the\n"+
				"  binary from the directory holding configs/.\n"+
				"  To choose a connection interactively, remove DB_DIALECT and %s and\n"+
				"  start again - the installer is served in its place.",
			cfg.Dialect, appconfig.DatasourceFile)
	}

	userRepo := sqldb.NewUserRepository(db, cfg.Dialect)
	roleRepo := sqldb.NewRoleRepository(db, cfg.Dialect)
	sessionRepo := sqldb.NewSessionRepository(db, cfg.Dialect)
	projectRepo := sqldb.NewProjectRepository(db, cfg.Dialect)
	timesheetRepo := sqldb.NewTimesheetRepository(db, cfg.Dialect)
	settingsRepo := sqldb.NewSettingsRepository(db, cfg.Dialect)

	tokenRepo := sqldb.NewAPITokenRepository(db, cfg.Dialect)
	passkeyRepo := sqldb.NewPasskeyRepository(db, cfg.Dialect)

	auth := appservice.NewAuthService(userRepo, roleRepo)
	apiTokens := appservice.NewAPITokenService(tokenRepo, userRepo, auth)
	passkeys := appservice.NewPasskeyService(passkeyRepo, userRepo, auth)
	settingsService := appservice.NewSettingsService(settingsRepo, roleRepo, cfg.AppName)
	setup := appservice.NewSetupService(settingsService, userRepo)

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
		Logs:       rest.NewLogHandler(logs, authorizer),
		Passkeys: rest.NewPasskeyHandler(passkeys, sessions, authorizer,
			// What the device's prompt calls this installation. The
			// administered title if there is one, so a person sees the name
			// they know rather than a default.
			func(ctx *gofr.Context) string {
				branding, err := settingsService.Branding(ctx)
				if err != nil || branding.Title == "" {
					return cfg.AppName
				}

				return branding.Title
			}),
		Settings: rest.NewSettingsHandler(settingsService, authorizer, limits, cfg.Dialect, version,
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
