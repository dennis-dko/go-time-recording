// Package harness starts the real application for a test to talk to.
//
// It is shared by the integration tests, which drive it over HTTP, and the
// browser tests, which drive it through a browser. Both want the same thing:
// the compiled binary, its own database, a free port, and a readable failure
// when it does not come up.
//
// It lives outside _test.go files so both packages can import it. That means
// it is compiled into ordinary builds, which is why it holds no test logic of
// its own - only the plumbing for starting and stopping an instance.
package harness

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

const (
	// StartupTimeout is long enough for migrations on a cold PostgreSQL, short
	// enough that a hung start-up is reported rather than waited out.
	StartupTimeout = 60 * time.Second

	// AdminEmail and AdminPassword are what a fresh installation creates.
	AdminEmail    = "admin@local"
	AdminPassword = "changeme123"

	// DSNEnv points the whole suite at a database server instead of SQLite.
	// The scheme picks the dialect:
	//
	//	postgres://gtr:secret@localhost:55432/go-time-recording?sslmode=disable
	//	mysql://root:secret@localhost:53306/go-time-recording
	//
	// The account has to be allowed to CREATE DATABASE, because each test gets
	// its own. On PostgreSQL the owner already can; on MySQL an ordinary user
	// cannot, so use root there - it is a throwaway server either way.
	DSNEnv = "GTR_TEST_DSN"
)

// App is a running instance.
type App struct {
	t       *testing.T
	baseURL string
	dir     string
	cmd     *exec.Cmd
	logs    *bytes.Buffer

	// metricsPort is the one this instance was given. Its own listener, on its
	// own port, outside the middleware chain - so a test that wants to read what
	// the application publishes cannot get there through BaseURL.
	metricsPort int

	// dbDriver and dbDSN reach the same database the instance is using. See DB.
	dbDriver string
	dbDSN    string

	// marker is the name this instance answers with, so a reply can be told from
	// a neighbour's. Empty when the caller named the instance itself. See
	// waitUntilReady.
	marker string
}

// DB opens a second connection to the database this instance is using.
//
// Almost everything should be asked over HTTP: that is what the suite is for, and a
// test that reaches around the API can pass while the application is broken. This is
// for the one question HTTP cannot answer - whether a row is still there.
//
// Deleting an account has to take its recorded time with it. What must not happen is
// a half-deletion, where the account is gone and its hours are left pointing at an id
// that no longer exists. Nobody can see those hours through the API, because nobody
// can read anybody else's time at all, and that is exactly why an orphan would sit
// there unnoticed until it turned up in a total or broke a foreign key on the next
// dialect. So the cascade is checked against the thing that cascades.
//
// Returns nil for an instance started without a database.
func (a *App) DB(t *testing.T) *sql.DB {
	t.Helper()

	if a.dbDriver == "" {
		return nil
	}

	db, err := sql.Open(a.dbDriver, a.dbDSN)
	if err != nil {
		t.Fatalf("cannot open the instance's database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("cannot reach the instance's database: %v", err)
	}

	return db
}

// Rows counts what matches, for a table this suite has to look inside.
func (a *App) Rows(t *testing.T, table, where string, args ...any) int {
	t.Helper()

	db := a.DB(t)
	if db == nil {
		t.Fatal("this instance has no database")
	}

	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}

	// Placeholders differ by dialect, and this takes ? because that is what the
	// application's own queries are written in.
	if a.dbDriver == "postgres" {
		for i := 1; strings.Contains(query, "?"); i++ {
			query = strings.Replace(query, "?", fmt.Sprintf("$%d", i), 1)
		}
	}

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}

	return count
}

// BaseURL is where the instance answers, e.g. http://localhost:54321
func (a *App) BaseURL() string { return a.baseURL }

// MetricsPort is where this instance serves /metrics.
func (a *App) MetricsPort() int { return a.metricsPort }

// Log returns what the instance has written so far, for a failure message.
func (a *App) Log() string { return a.logs.String() }

// binaryPath is set by Build and reused by every Start in the package.
var binaryPath atomic.Pointer[string]

// Build compiles the application once, for the whole test binary to share.
// Call it from TestMain.
func Build() (cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "gtr-harness-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create a temporary directory: %w", err)
	}

	path := filepath.Join(dir, "go-time-recording"+exeSuffix())

	build := exec.Command("go", "build", "-o", path, "./cmd/main.go")
	build.Dir = RepoRoot()
	build.Stderr = os.Stderr

	if err := build.Run(); err != nil {
		_ = os.RemoveAll(dir)

		return nil, fmt.Errorf("cannot build the application: %w", err)
	}

	binaryPath.Store(&path)

	return func() { _ = os.RemoveAll(dir) }, nil
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}

	return ""
}

// RepoRoot is two levels up from a package under test/.
func RepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	return filepath.Join(wd, "..", "..")
}

// Start launches a fresh instance with its own database and configuration.
//
// Each test gets its own: they create users, change the instance timezone and
// complete the setup wizard, and sharing one would make the outcome depend on
// which test ran first.
func Start(t *testing.T, env ...string) *App {
	t.Helper()

	return start(t, true, env...)
}

// StartUnconfigured launches an instance with no database configured at all, so
// it serves its installer rather than the application.
//
// The one case Start cannot cover: it always supplies a database, which is what
// every other test wants and exactly what the installer must not have.
func StartUnconfigured(t *testing.T, env ...string) *App {
	t.Helper()

	return start(t, false, env...)
}

// Dir is the working directory of the instance, which is where its configs/ and
// - for SQLite - its database file live.
func (a *App) Dir() string { return a.dir }

// start launches an instance, trying again if it lost a race for its port.
//
// FreePort asks the operating system for a port, closes the listener and hands
// the number over - so between that close and the application's bind there is a
// window in which anything else on the machine can take it. With one instance per
// test and a suite that starts dozens, that window is hit: the application logs
// "address already in use", listens to nothing, and the test waits out its whole
// start-up deadline before failing for what looks like a slow machine.
//
// Nothing can close that window from here; a port cannot be reserved and handed
// to a child process. What can be done is to notice and try another one, which is
// what a person would do.
func start(t *testing.T, withDatabase bool, env ...string) *App {
	t.Helper()

	const attempts = 3

	for attempt := 1; ; attempt++ {
		app, err := startOnce(t, withDatabase, env...)
		if err == nil {
			return app
		}

		if !errors.Is(err, errPortTaken) || attempt == attempts {
			t.Fatalf("cannot start the application: %v", err)
		}

		// Whatever took the port is welcome to it. This one is stopped now rather
		// than at the end of the test, so a discarded attempt is not left running
		// beside the one that works.
		app.stop()
	}
}

func startOnce(t *testing.T, withDatabase bool, env ...string) (*App, error) {
	t.Helper()

	path := binaryPath.Load()
	if path == nil {
		t.Fatal("harness.Build() was not called from TestMain")
	}

	dir := t.TempDir()

	// GoFr resolves ./configs relative to the working directory, so the real
	// configuration is copied beside the binary rather than approximated.
	copyConfigs(t, filepath.Join(RepoRoot(), "cmd", "configs"), filepath.Join(dir, "configs"))

	port := FreePort(t)
	metricsPort := FreePort(t)

	// A name nothing else is answering with, so readiness can tell this
	// instance's reply from the reply of one that already had the port.
	//
	// Not set when the caller named the instance: a test that passes APP_NAME is
	// testing something about that name, and overwriting it would break the thing
	// it came to look at. Those fall back to reading the log, as everything did
	// before.
	marker := ""

	if !namesTheApp(env) {
		marker = fmt.Sprintf("harness-%d-%d", os.Getpid(), instances.Add(1))
	}

	cmd := exec.Command(*path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"APP_ENV=", // only .env, so a stray .local.env cannot change the outcome
		fmt.Sprintf("HTTP_PORT=%d", port),
		fmt.Sprintf("METRICS_PORT=%d", metricsPort),
		"LOG_LEVEL=WARN",
	)

	if marker != "" {
		cmd.Env = append(cmd.Env, "APP_NAME="+marker)
	}

	driver, driverDSN := "", ""

	if withDatabase {
		name := filepath.Join(dir, "test")

		cmd.Env = append(cmd.Env, "DB_DIALECT=sqlite", "DB_NAME="+name)

		// The same rule the application applies to DB_NAME, so both open one file.
		driver, driverDSN = "sqlite", "file:"+name+".db"

		if dsn := os.Getenv(DSNEnv); dsn != "" {
			var env []string

			env, driver, driverDSN = serverEnv(t, dsn)
			cmd.Env = append(cmd.Env, env...)
		}
	} else {
		// Explicitly blank rather than merely absent: the process inherits this
		// test run's own environment, which on a developer's machine may well
		// have DB_DIALECT set from something else.
		cmd.Env = append(cmd.Env, "DB_DIALECT=", "DB_NAME=", "SETUP_TOKEN=")
	}

	cmd.Env = append(cmd.Env, env...)

	logs := &bytes.Buffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs

	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the application: %v", err)
	}

	a := &App{
		t: t,
		// localhost rather than 127.0.0.1: browsers treat both as a secure
		// context, but WebAuthn rejects an IP address as a relying party
		// identifier - "SecurityError: This is an invalid domain". Passkey
		// tests would fail for a reason that has nothing to do with the code
		// under test.
		baseURL:     fmt.Sprintf("http://localhost:%d", port),
		metricsPort: metricsPort,
		dir:         dir,
		cmd:         cmd,
		logs:        logs,
		dbDriver:    driver,
		dbDSN:       driverDSN,
		marker:      marker,
	}

	t.Cleanup(a.stop)

	return a, a.waitUntilReady()
}

// serverEnv gives this test its own database on the configured server and returns
// the DB_* variables pointing at it, plus the driver and connection string a test
// needs to open the same database itself.
func serverEnv(t *testing.T, dsn string) (env []string, driver, driverConn string) {
	t.Helper()

	dialect := "postgres"
	trimmed := dsn

	switch {
	case strings.HasPrefix(dsn, "mysql://"):
		dialect = "mysql"
		trimmed = strings.TrimPrefix(dsn, "mysql://")
	case strings.HasPrefix(dsn, "postgresql://"):
		trimmed = strings.TrimPrefix(dsn, "postgresql://")
	case strings.HasPrefix(dsn, "postgres://"):
		trimmed = strings.TrimPrefix(dsn, "postgres://")
	default:
		t.Fatalf("%s needs a postgres:// or mysql:// scheme: %q", DSNEnv, dsn)
	}

	credentials, rest, ok := strings.Cut(trimmed, "@")
	if !ok {
		t.Fatalf("%s is not a database URL: %q", DSNEnv, dsn)
	}

	user, password, _ := strings.Cut(credentials, ":")
	hostPort, nameAndQuery, _ := strings.Cut(rest, "/")
	host, port, _ := strings.Cut(hostPort, ":")
	adminDB, _, _ := strings.Cut(nameAndQuery, "?")

	name := createTestDatabase(t, dialect, driverDSN(dialect, user, password, host, port, adminDB))

	env = []string{
		"DB_DIALECT=" + dialect,
		"DB_HOST=" + host,
		"DB_PORT=" + port,
		"DB_USER=" + user,
		"DB_PASSWORD=" + password,
		"DB_NAME=" + name,
	}

	// Only PostgreSQL reads this, and passing it to MySQL puts an unknown
	// parameter in its DSN.
	if dialect == "postgres" {
		env = append(env, "DB_SSL_MODE=disable")
	}

	return env, dialect, driverDSN(dialect, user, password, host, port, name)
}

// driverDSN builds the connection string each driver expects. They disagree:
// lib/pq takes a URL, go-sql-driver/mysql takes user:pass@tcp(host:port)/name.
func driverDSN(dialect, user, password, host, port, name string) string {
	if dialect == "mysql" {
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", user, password, host, port, name)
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, name)
}

// databaseCounter numbers the throwaway databases. Tests can run in parallel,
// so it is incremented atomically.
var databaseCounter atomic.Int64

// createTestDatabase creates an empty database and drops it when the test ends.
func createTestDatabase(t *testing.T, dialect, adminDSN string) string {
	t.Helper()

	db, err := sql.Open(dialect, adminDSN)
	if err != nil {
		t.Fatalf("cannot reach the %s server: %v", dialect, err)
	}

	defer func() { _ = db.Close() }()

	// Lower case and no punctuation: an identifier that would need quoting
	// everywhere it appears is a poor choice for a name generated in a test.
	name := fmt.Sprintf("gtr_it_%d_%d", time.Now().UnixNano()%1e9, databaseCounter.Add(1))

	if _, err := db.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("cannot create the test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		cleanup, err := sql.Open(dialect, adminDSN)
		if err != nil {
			return
		}

		defer func() { _ = cleanup.Close() }()

		drop := "DROP DATABASE IF EXISTS " + name

		// PostgreSQL refuses to drop a database something is still connected
		// to, and the application may still be shutting down. MySQL has no such
		// clause and needs none.
		if dialect == "postgres" {
			drop += " WITH (FORCE)"
		}

		_, _ = cleanup.Exec(drop)
	})

	return name
}

// handedOut is every port this process has already given to an instance.
//
// The operating system will hand the same number out again once the listener
// below is closed, and it does: a suite that starts dozens of instances asks
// dozens of times, and the numbers repeat. Two instances on one port is the
// whole failure this file guards against, so the cheapest half of the guard is
// simply not to suggest a port twice.
var handedOut sync.Map

// FreePort asks the operating system for a port nothing is using.
//
// Nothing else in this process, at least. The listener has to be closed before
// the number can be handed to a child, and in that window anything on the
// machine may take it - which is what start's retry and waitUntilReady's
// identity check are for.
func FreePort(t *testing.T) int {
	t.Helper()

	const attempts = 20

	for range attempts {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("cannot find a free port: %v", err)
		}

		port := listener.Addr().(*net.TCPAddr).Port

		_ = listener.Close()

		if _, seen := handedOut.LoadOrStore(port, struct{}{}); !seen {
			return port
		}
	}

	t.Fatalf("could not find a port this run has not already used, after %d tries",
		attempts)

	return 0
}

func copyConfigs(t *testing.T, from, to string) {
	t.Helper()

	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("cannot create %s: %v", to, err)
	}

	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("cannot read %s: %v", from, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			t.Fatalf("cannot read %s: %v", entry.Name(), err)
		}

		if err := os.WriteFile(filepath.Join(to, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", entry.Name(), err)
		}
	}
}

// waitUntilReady polls until the instance answers, and reports its log if it
// never does - a start-up failure is otherwise invisible behind a timeout.
// waitUntilReady blocks until the instance answers, and says what stopped it if
// it never does.
//
// It reports rather than failing the test outright, because one of the answers is
// worth acting on instead: a port that was free when this process asked for one
// and taken by the time the application bound it. See start.
func (a *App) waitUntilReady() error {
	a.t.Helper()

	deadline := time.Now().Add(StartupTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	// When the identity question first started getting nowhere. See the unclear
	// case below.
	var unclearSince time.Time

	for time.Now().Before(deadline) {
		if a.cmd.ProcessState != nil && a.cmd.ProcessState.Exited() {
			return fmt.Errorf("the application exited during start-up:\n%s", a.logs.String())
		}

		// The application says this once and then carries on, listening to
		// nothing, so waiting out the whole deadline is waiting for something that
		// has already failed.
		if err := a.startupFailure(); err != nil {
			return err
		}

		resp, err := client.Get(a.baseURL + "/")
		if err == nil {
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				// An answer is not proof it came from this instance.
				//
				// The port was free when it was asked for and is bound by a
				// different process, so the address can belong to another test's
				// instance that is still running - and that one answers 200 to
				// anything. Accepting it means the test talks to somebody else's
				// database, and the failure lands three steps later as an account
				// that already exists.
				//
				// Reading the log was the first attempt at this and it is a race of
				// its own: it only works if our process has already written that it
				// could not bind by the time the neighbour answers. The neighbour is
				// already running, so it usually answers first. What that cost was a
				// suite where one test in twenty failed three steps later, saying an
				// account already existed.
				//
				// So the answer is identified instead of merely counted. Every
				// instance is started under a name nothing else is using, and it
				// says that name when asked what it is called. A reply carrying
				// somebody else's name is somebody else's instance.
				switch a.whoAnswered(client) {
				case answerIsAStranger:
					return errPortTaken

				case answerIsUnclear:
					// Not an answer, so not a reason to accept the address: under
					// load the identity request is the first thing to time out, and
					// reading that as "ours" accepts whatever is on the port, which
					// is the mix-up this check exists to stop.
					//
					// Bounded, though. An instance in installer mode answers "/"
					// perfectly well and cannot answer this at all - there is no
					// database behind it - so insisting for ever would leave it a
					// port it could never be ready on. After a few seconds of
					// getting nowhere this stops insisting and falls back to the
					// log, which is what everything did before.
					if unclearSince.IsZero() {
						unclearSince = time.Now()
					}

					if time.Since(unclearSince) < identityGrace {
						time.Sleep(150 * time.Millisecond)

						continue
					}

				case answerIsOurs:
				}

				// Still worth the second look for the instances that carry no name
				// of their own, and for anything the check above cannot see.
				if failed := a.startupFailure(); failed != nil {
					return failed
				}

				return nil
			}
		}

		time.Sleep(150 * time.Millisecond)
	}

	return fmt.Errorf("the application did not become ready within %s:\n%s",
		StartupTimeout, a.logs.String())
}

// Who answered on this instance's address.
type answeredBy int

const (
	// answerIsOurs means the reply carried this instance's own name, or there is
	// no name to check against.
	answerIsOurs answeredBy = iota

	// answerIsAStranger means the reply carried somebody else's name.
	answerIsAStranger

	// answerIsUnclear means the question could not be put or could not be
	// answered - which is not the same as an answer, and must not be read as one.
	answerIsUnclear
)

// whoAnswered asks what is listening on this instance's address what it is
// called.
//
// Every instance is started under a name nothing else is using and says that
// name through the branding endpoint, which is public - so this is one request
// at a moment where no session exists yet.
//
// Three answers rather than two, and the third is the one that matters. The
// request has a short deadline and this suite now runs several cases at once, so
// under load the identity request is the first thing to time out. Reading a
// timeout as "ours" accepts whatever is on the port, which is exactly the
// mix-up this check exists to prevent - and it surfaces three steps later as an
// account that already exists.
//
// A reply that is not 200 is different again: the installer runs before there is
// a database and never registers the route that would answer, so there the
// question cannot be put at all and the log is what is left to go on.
func (a *App) whoAnswered(client *http.Client) answeredBy {
	if a.marker == "" {
		return answerIsOurs
	}

	resp, err := client.Get(a.baseURL + "/api/v1/branding")
	if err != nil {
		return answerIsUnclear
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return answerIsOurs
	}

	var body struct {
		Data struct {
			Title string `json:"title"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return answerIsUnclear
	}

	switch body.Data.Title {
	case "":
		return answerIsOurs
	case a.marker:
		return answerIsOurs
	default:
		return answerIsAStranger
	}
}

// instances counts the instances this process has started, so each can be given
// a name of its own.
var instances atomic.Uint64

// namesTheApp reports whether the caller set APP_NAME itself.
func namesTheApp(env []string) bool {
	for _, variable := range env {
		if strings.HasPrefix(variable, "APP_NAME=") {
			return true
		}
	}

	return false
}

// identityGrace is how long to keep asking an address what it is called before
// giving up on the question.
//
// Long enough that a busy machine gets its answer, short enough that an instance
// which cannot answer at all - the installer, which runs before there is a
// database - is not left waiting out its whole start-up deadline for a question
// nothing was ever going to reply to.
const identityGrace = 8 * time.Second

// errPortTaken is the one start-up failure worth simply trying again.
var errPortTaken = errors.New("the port was taken between choosing it and binding it")

// startupFailure is what the instance has already said went wrong, if anything.
//
// Its own function so the recognition can be tested without racing a port: what
// is being recognised is a sentence the application writes, and the sentence is
// the part that can change underneath this.
func (a *App) startupFailure() error {
	// Two wordings for one thing: the first is what a Unix returns and the second
	// is Windows', which this suite also runs on. Recognising only the first made
	// the guard silently do nothing on the platform where a developer is most
	// likely to be running it by hand.
	for _, said := range []string{
		"address already in use",
		"normally permitted",
	} {
		if strings.Contains(a.logs.String(), said) {
			return errPortTaken
		}
	}

	return nil
}

// logBuffer builds a buffer holding one line, for the tests of the above.
func logBuffer(line string) *bytes.Buffer {
	return bytes.NewBufferString(line)
}

func (a *App) stop() {
	if a.cmd.Process == nil {
		return
	}

	_ = a.cmd.Process.Kill()
	_, _ = a.cmd.Process.Wait()

	a.removeDir()
}

// removeDir deletes the instance's directory before testing.T gets to it.
//
// Windows releases a file handle some time after the process holding it dies, and
// a SQLite database in write-ahead logging has three files rather than one. So
// t.TempDir's own cleanup regularly ran a moment too early and failed the test
// with "The directory is not empty" - a failure about the operating system, on a
// test that had already passed.
//
// Retried briefly rather than slept through, and the outcome is ignored: if it
// still cannot be removed, t.TempDir will report it, which is the behaviour
// without this.
func (a *App) removeDir() {
	if a.dir == "" {
		return
	}

	deadline := time.Now().Add(5 * time.Second)

	for {
		if err := os.RemoveAll(a.dir); err == nil {
			return
		}

		if time.Now().After(deadline) {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}
