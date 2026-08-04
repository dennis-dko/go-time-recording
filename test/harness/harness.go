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
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
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

func start(t *testing.T, withDatabase bool, env ...string) *App {
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

	cmd := exec.Command(*path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"APP_ENV=", // only .env, so a stray .local.env cannot change the outcome
		fmt.Sprintf("HTTP_PORT=%d", port),
		fmt.Sprintf("METRICS_PORT=%d", metricsPort),
		"LOG_LEVEL=WARN",
	)

	if withDatabase {
		cmd.Env = append(cmd.Env,
			"DB_DIALECT=sqlite",
			"DB_NAME="+filepath.Join(dir, "test"),
		)

		if dsn := os.Getenv(DSNEnv); dsn != "" {
			cmd.Env = append(cmd.Env, serverEnv(t, dsn)...)
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
	}

	t.Cleanup(a.stop)
	a.waitUntilReady()

	return a
}

// serverEnv gives this test its own database on the configured server and
// returns the DB_* variables pointing at it.
func serverEnv(t *testing.T, dsn string) []string {
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

	env := []string{
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

	return env
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

// FreePort asks the operating system for a port nothing is using.
func FreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot find a free port: %v", err)
	}

	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
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
func (a *App) waitUntilReady() {
	a.t.Helper()

	deadline := time.Now().Add(StartupTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		if a.cmd.ProcessState != nil && a.cmd.ProcessState.Exited() {
			a.t.Fatalf("the application exited during start-up:\n%s", a.logs.String())
		}

		resp, err := client.Get(a.baseURL + "/")
		if err == nil {
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return
			}
		}

		time.Sleep(150 * time.Millisecond)
	}

	a.t.Fatalf("the application did not become ready within %s:\n%s", StartupTimeout, a.logs.String())
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
