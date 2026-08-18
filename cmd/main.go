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
	"sync/atomic"
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
	"modernc.org/sqlite"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/directory"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/logsink"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/migrations"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/tlsserver"
	v1 "github.com/dennis-dko/go-time-recording/internal/interface/api/v1"
	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
	"github.com/dennis-dko/go-time-recording/internal/interface/installer"
	"github.com/dennis-dko/go-time-recording/internal/interface/web"
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

// sqliteBusyTimeout is how long a writer waits for another writer to finish
// before giving up.
//
// Five seconds is far longer than any statement this application runs and far
// shorter than a person's patience. The alternative to waiting is the request
// failing, so the only way to be wrong here is to be too short.
const sqliteBusyTimeout = 5 * time.Second

// makeSQLiteWait gives every SQLite connection a busy timeout.
//
// SQLite serialises writers. Without a timeout the second one is refused outright
// with "database is locked (5) (SQLITE_BUSY)", which surfaces as a 500 for
// somebody whose only mistake was saving while somebody else saved - and, worse,
// as a failed session lookup that reads as "not signed in".
//
// A busy timeout is normally set in the connection string, and GoFr builds that
// itself - "file:name.db", with nowhere to put one. The driver's connection hook
// is the way in: it runs after every connection the pool opens, which is what
// matters, because a pragma belongs to one connection and the pool makes more of
// them whenever it needs to. Registered before gofr.New(), which is when the first
// connection is opened.
//
// Costs nothing on the other dialects: the hook belongs to the SQLite driver and
// is never called if nothing opens a SQLite connection.
func makeSQLiteWait() {
	pragma := fmt.Sprintf("PRAGMA busy_timeout = %d", sqliteBusyTimeout.Milliseconds())

	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, _ string) error {
		if _, err := conn.ExecContext(context.Background(), pragma, nil); err != nil {
			// Returned rather than swallowed: it fails the connection that could
			// not be configured, which is visible, instead of leaving a pool of
			// connections that quietly differ from each other.
			return fmt.Errorf("setting the SQLite busy timeout: %w", err)
		}

		return nil
	})
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
// Concurrent *writers* still serialise, and that is what makeSQLiteWait covers:
// they now queue instead of one of them being refused.
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
	// Before anything else, and before the log is captured: this answers and
	// exits.
	//
	// It exists for the update to use. Replacing a binary and then restarting
	// into it is a bet that the new one runs at all, and if it does not, the
	// screen that could have put the old one back has gone with it. So the
	// update runs the downloaded file with this first and only keeps it if it
	// answers - which is a small thing to ask of a program about to become this
	// one. Being able to ask a binary what it is from a shell is worth having
	// anyway.
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(version)
		os.Exit(0)
	}

	// The way back, and it has to be here rather than behind the interface: the
	// case it exists for is a version that installed and will not serve, where
	// there is no interface to press anything in. One flag, no database, no
	// configuration - it moves two files and says what it did.
	if len(os.Args) > 1 && (os.Args[1] == "--rollback" || os.Args[1] == "-rollback") {
		if err := selfupdate.Rollback(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		fmt.Println("the previous version is back in place; start it again")
		os.Exit(0)
	}

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

	// What a previous update left behind, now that this process is the new
	// version. On Windows the old binary cannot be deleted while it is running,
	// so the swap renames it aside and this is the first moment it can go.
	selfupdate.Cleanup()

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

	// The administered metrics and tracing settings, read out of the database
	// before GoFr opens it: GoFr binds the metrics port and builds the trace
	// exporter inside gofr.New(), so this is the last moment either can be
	// influenced. See the config package for why an empty value overrides a
	// configured one.
	//
	// The error is carried rather than reported: there is nowhere to report it yet,
	// because the logger belongs to the application that does not exist until the
	// next line.
	telemetry, telemetryErr := appconfig.StoredTelemetry(context.Background(), ds)
	if telemetryErr == nil {
		if err := appconfig.ApplyTelemetry(telemetry); err != nil {
			die(restoreOutput, "cannot apply the administered telemetry settings: %v", err)
		}
	}

	// The log level is the one telemetry setting that does not have to wait for a
	// restart, and this is what buys that.
	//
	// The framework decides what to emit from a field it reads without
	// synchronisation, so changing it while requests are in flight is a data
	// race - which is why this does not use its ChangeLevel. Instead the
	// framework is left at its most verbose and the level is applied on the way
	// out, in the single goroutine draining the captured output. Raising or
	// lowering it is then a store in one place with a mutex around it, and takes
	// effect on the next line.
	//
	// Only where the output is actually captured. Without capture there is
	// nothing between the framework and the console to apply a level, so the
	// framework keeps deciding and the setting keeps needing a restart - which is
	// the behaviour every version until now had.
	//
	// The file's own level is read first and kept: once LOG_LEVEL has been
	// widened below, there is no reading it back, and it is what an administrator
	// clearing the field to "follow the configuration file" is asking for.
	var applyLogLevel func(string)

	if restoreOutput != nil {
		fileLogLevel := appconfig.EffectiveLogLevel(model.Telemetry{})

		applyLogLevel = func(level string) {
			if strings.TrimSpace(level) == "" {
				level = fileLogLevel
			}

			logs.SetLevel(level)
		}

		applyLogLevel(appconfig.EffectiveLogLevel(telemetry))

		if err := os.Setenv("LOG_LEVEL", "DEBUG"); err != nil {
			die(restoreOutput, "cannot open the log for filtering: %v", err)
		}
	}

	// Before gofr.New(), which opens the first connection: the hook has to be in
	// place before there is anything to configure.
	makeSQLiteWait()

	// The build's own version, where GoFr will look for it. GoFr puts APP_VERSION
	// into the body of /.well-known/health, and the only place that variable was
	// ever set is configs/.env, which ships "0.0.1" and is baked into the image -
	// so every release answered a monitoring check with 0.0.1 while its footer
	// correctly said v1.2.3. Exported rather than passed, because GoFr reads it
	// from its configuration and lets the real environment win over the file, the
	// same lever ApplyDatasource uses.
	if err := os.Setenv("APP_VERSION", version); err != nil {
		die(restoreOutput, "cannot publish the build version: %v", err)
	}

	app := gofr.New()

	if telemetryErr != nil {
		// Debug rather than a warning, because the ordinary cause is a first
		// start: the settings table is created by the migrations below, so there
		// is nothing to read yet. A database that is genuinely unreachable was
		// refused above, by a message that says so.
		app.Logger().Debugf("no administered metrics or tracing settings were read (%v); "+
			"the configuration file's values apply", telemetryErr)
	}

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

	timerRepo := sqldb.NewTimerRepository(db, cfg.Dialect)
	tokenRepo := sqldb.NewAPITokenRepository(db, cfg.Dialect)
	passkeyRepo := sqldb.NewPasskeyRepository(db, cfg.Dialect)

	auth := appservice.NewAuthService(userRepo, roleRepo)
	apiTokens := appservice.NewAPITokenService(tokenRepo, userRepo, auth)
	passkeys := appservice.NewPasskeyService(passkeyRepo, userRepo, auth)
	settingsService := appservice.NewSettingsService(settingsRepo, roleRepo, cfg.AppName)

	// The administered directory schedule wins over the configuration file, the
	// same way the administered database connection and log level do.
	//
	// Resolved into cfg here, before anything reads it: a cron job is registered
	// while the application starts and cannot be added to a scheduler that is
	// already running - which is why changing it needs a restart - and the restart
	// card compares what is stored against what cfg says this process is running.
	if stored, err := settingsService.LDAP(context.Background()); err != nil {
		// Not fatal, and not loud: on a first start the settings table has only
		// just been created by the migrations, so there is nothing to read yet.
		app.Logger().Debugf("could not read the administered directory schedule (%v); "+
			"the configuration file's value applies", err)
	} else if stored.SyncSchedule != "" {
		cfg.LDAPSyncSchedule = stored.SyncSchedule
	}
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
		LDAPSyncMaxDeleteRatio: cfg.LDAPSyncMaxDeleteRatio,
	})

	// The framework already measures the machinery; these measure the work. See
	// the service package for why each one is here and why none of them carries
	// a person's name as a label.
	registerBusinessMetrics(app)

	sessions := appservice.NewSessionService(userRepo, roleRepo, sessionRepo, auth, cfg.SessionLifetime).
		WithExternalAuth(ldapClient, model.RoleUser).
		WithLimits(limits).
		WithMetrics(app.Metrics())

	ldapSync := appservice.NewLDAPSyncService(ldapClient, userRepo, roleRepo, timesheetRepo,
		userRepo, cfg.LDAPSyncMaxDeleteRatio, model.RoleUser).
		WithLimits(limits).
		WithMetrics(app.Metrics())

	users := appservice.NewUserApplicationService(userRepo, roleRepo, timesheetRepo, userRepo)
	roles := appservice.NewRoleApplicationService(roleRepo)
	projects := appservice.NewProjectApplicationService(projectRepo, timesheetRepo, timerRepo)
	timesheets := appservice.NewTimesheetApplicationService(
		timesheetRepo, userRepo, projectRepo, cfg.MaxDailyHours).
		WithLimits(limits).
		WithMetrics(app.Metrics())
	overtime := appservice.NewOvertimeService(timesheetRepo, userRepo)

	// Through the timesheet service rather than the repository, so a timer
	// booking meets exactly the rules a typed one meets.
	timers := appservice.NewTimerService(timerRepo, timesheets)
	statistics := appservice.NewStatisticsService(timesheetRepo, projectRepo)

	// The timesheet service is passed in rather than reimplemented: the import has
	// to enforce the rules the API enforces, and the only way to be sure of that is
	// to call them.
	workbook := appservice.NewWorkbookService(timesheetRepo, userRepo, projectRepo, timesheets)
	projectSheets := appservice.NewProjectWorkbookService(projects)
	userSheets := appservice.NewUserWorkbookService(userRepo, roleRepo, users)
	roleSheets := appservice.NewRoleWorkbookService(roleRepo, roles)

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

		// The metrics and tracing settings were read before this application
		// existed, and that read is allowed to fail: on a first start the table
		// below has only just been created, which says nothing and deserves no
		// mention. What must not pass silently is a failure with something
		// actually stored - the screen would then show settings this process is
		// not running on, and nothing anywhere would say why.
		//
		// Read again here, through GoFr's own connection and after the
		// migrations, which is what tells the two cases apart.
		if telemetryErr != nil {
			stored, err := settingsService.Telemetry(ctx)
			if err == nil && stored.Administered() {
				// The reason is carried through rather than summarised: it says
				// whether this is worth a restart or needs the settings corrected
				// first, and the two have different remedies.
				ctx.Logger.Warnf(
					"the administered metrics and tracing settings were not applied to this process "+
						"(%v), which is therefore running on the configuration file's values",
					telemetryErr)
			}
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

	// Built here rather than inline, because both the middleware that reads it and
	// the handler that clears it need the same instance - two would mean the
	// switch takes effect for one of them and not the other.
	maintenanceState := rest.CachedMaintenanceState(settingsService.Maintenance)

	// Order matters, and GoFr applies middleware in registration order:
	// security headers first so they are set even on a rejected request, then
	// the rate limit, then the cookie queue wrapping the two authentication
	// paths, and only then the UI.
	// Where an announcement to everybody starts. One per process, held here
	// because two things need it: the update handler, which says what is about to
	// happen, and the stream middleware, which carries it.
	hub := announce.New()

	// And its end. Every other request finishes by itself, which is what lets the
	// HTTP server drain before exiting; an announcement stream does not, by
	// design, so a shutdown would sit out its whole timeout on connections that
	// are behaving exactly as intended. On the restart path that would be added
	// straight onto the time the application is unavailable.
	//
	// A second listener for the same signals GoFr handles. signal.Notify supports
	// that, and the alternative is reaching into how GoFr shuts down.
	go func() {
		stopping := make(chan os.Signal, 1)
		signal.Notify(stopping, os.Interrupt, syscall.SIGTERM)

		<-stopping
		hub.Close()
	}()

	// Outermost, because everything after it is written for traffic that arrived
	// through the front door. While HTTPS is served from this process, GoFr's
	// plain port answers only the loopback address that front end dials from,
	// and sends the network to the encrypted address instead.
	//
	// The flag is set below, once the HTTPS listener is bound rather than once it
	// has been asked for: an installation whose port 443 was refused still serves
	// on the plain one, and redirecting it to a port with nothing behind it would
	// turn a warning in the log into an outage.
	var httpsFrontEnd atomic.Bool

	app.UseMiddleware(tlsserver.KeepThePlainPortLocal(httpsFrontEnd.Load, cfg.TLSPort))
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

	// After both authentication paths, because the exemption for whoever
	// administers the installation needs to know who is calling - placed earlier
	// it would turn away the only people who can end maintenance mode. Before the
	// UI, so the assets are still served and the page can render the notice.
	app.UseMiddleware(rest.MaintenanceMiddleware(maintenanceState))

	// The one thing this application says without being asked. After the session
	// middleware, which is what makes a stream belong to somebody, and before the
	// interface, which would otherwise answer for a path it does not own.
	app.UseMiddleware(rest.EventStream(hub))

	if cfg.UIEnabled {
		// GoFr's AddStaticFiles only serves a directory from disk, which would
		// defeat the single-binary goal, so the embedded UI is installed as
		// middleware instead. The assets stay public: the sign-in form has to
		// be reachable before there is a session.
		// The served document carries this installation's own name and mark,
		// rather than the shipped defaults with a script correcting them a round
		// trip later.
		app.UseMiddleware(web.Handler(brandingFor(settingsService)))
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
		Restart:    rest.NewRestartHandler(settingsService, authorizer, cfg, ds, applyLogLevel != nil),
		Update: rest.NewUpdateHandler(authorizer,
			selfupdate.New(cfg.UpdateFeed, cfg.UpdateToken), hub, version, cfg.UpdateCheck),
		Timers:     rest.NewTimerHandler(timers, authorizer, instanceTimezone),
		Statistics: rest.NewStatisticsHandler(statistics, authorizer, instanceTimezone),
		Workbook:   rest.NewWorkbookHandler(workbook, authorizer, instanceTimezone),
		Sheets:     rest.NewSheetHandler(projectSheets, userSheets, roleSheets, authorizer),
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
		Settings: rest.NewSettingsHandler(settingsService, authorizer, limits,
			cfg.Dialect, cfg.Telemetry, version,
			ldapClient.Configure,
			func(ctx *gofr.Context, config model.LDAPConfig) error {
				return ldapClient.TestConnection(ctx, config)
			}).WithMaintenance(maintenanceState).
			WithLiveLogLevel(applyLogLevel, logs.Level),
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
		app.Logger().Infof("directory reconciliation scheduled at %q", cfg.LDAPSyncSchedule)

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

	// The nightly sweep that moved stale open entries to submitted is gone with the
	// review path it fed: an entry has no state any more, and there is nobody to
	// submit it to.

	if stop := startTLS(app, cfg); stop != nil {
		httpsFrontEnd.Store(true)

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

// registerBusinessMetrics declares the ones this application records itself.
//
// Declaring is not publishing, which is worth knowing before writing an alert
// against one of these. The registration creates the instrument; the exporter
// emits a series only once it has a value, so a fresh installation that has had
// no refused sign-in publishes no gtr_signin_failures_total at all rather than
// publishing it as zero.
//
// So "nothing has gone wrong yet" and "this metric does not exist" are the same
// empty query result here. An alert has to treat an absent series as absent
// rather than as a healthy zero - absent() in Prometheus - and a dashboard panel
// is empty until the first event rather than flat at zero.
func registerBusinessMetrics(app *gofr.App) {
	m := app.Metrics()

	// Buckets in hours, over the range a single entry can hold: the quarter and
	// half hours people actually book, then the working day, then the rest.
	m.NewHistogram(appservice.MetricHoursBooked,
		"Hours recorded per time entry.",
		0.25, 0.5, 1, 2, 4, 6, 8, 10, 12, 24)

	m.NewCounter(appservice.MetricSignInFailures,
		"Refused sign-ins, by reason.")
	m.NewCounter(appservice.MetricDirectoryAccounts,
		"Accounts the directory synchronisation created or deleted.")
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

	// Either a name Let's Encrypt can be asked about, or a certificate this
	// installation already has. Without one of the two there is nothing to serve
	// HTTPS with, and starting anyway would serve plain HTTP under a setting that
	// says otherwise.
	hasOwn := strings.TrimSpace(cfg.TLSCertFile) != "" && strings.TrimSpace(cfg.TLSKeyFile) != ""

	if len(cfg.TLSDomains) == 0 && !hasOwn {
		app.Logger().Errorf("TLS_ENABLED is true but neither TLS_DOMAINS nor " +
			"TLS_CERT_FILE and TLS_KEY_FILE are set; continuing without HTTPS")

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
		CertFile:  cfg.TLSCertFile,
		KeyFile:   cfg.TLSKeyFile,
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

// brandingFor answers what the instance is called, for the served document.
//
// Read per request, and deliberately not cached.
//
// It was cached for five seconds at first, on the reasoning that this is on the
// path of every page load. What that bought was one small SELECT of a table with
// a handful of rows; what it cost was that saving a logo and reloading to look at
// it showed the old one, which is exactly what somebody does immediately after
// saving. A correctness problem in the one workflow this feature has, traded for
// a query nobody would have noticed.
//
// The icon itself is cached hard by the browser instead, at an address carrying a
// fingerprint of its contents - so a repeated page load re-reads this row and
// fetches no image at all.
func brandingFor(settings *appservice.SettingsService) web.BrandingFunc {
	return func(ctx context.Context) web.Branding {
		branding, err := settings.Branding(ctx)
		if err != nil {
			// The database being unreachable is not a reason to serve no page. The
			// shipped title and mark are wrong but present, and every other screen
			// is about to report the real problem.
			return web.Branding{}
		}

		return web.Branding{
			// What the tab is called, which is the only title this document has.
			// The header's name is put there by the interface once it has loaded,
			// and the two are only the same until somebody sets them apart.
			Title: branding.TabName(),
			Logo:  branding.LogoDataURI,
			Icon:  branding.LogoIcon,
		}
	}
}
