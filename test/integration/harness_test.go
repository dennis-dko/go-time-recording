//go:build integration

// Package integration exercises the application the way a browser and a script
// do: over HTTP, against a real database, through the real binary.
//
// The unit tests already cover the rules in isolation. What they cannot cover
// is everything between a request arriving and a rule being reached - the
// middleware order, the CSRF check, the session cookie, the migrations, the
// embedded assets, the wiring in main.go. Every bug found in this project by
// running it rather than testing it lived in exactly that gap: a login screen
// that could not be dismissed, a booking dated in UTC, a directory that could
// take over the administrator account.
//
// So this starts the compiled binary as a subprocess and talks to it. Nothing
// is stubbed.
//
//	go test -tags integration ./test/integration
//	GTR_TEST_DSN=postgres://... go test -tags integration ./test/integration
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const (
	// Long enough for migrations on a cold PostgreSQL, short enough that a
	// hung start-up is reported rather than waited out.
	startupTimeout = 60 * time.Second

	adminEmail    = "admin@local"
	adminPassword = "changeme123"
)

// app is a running instance plus everything needed to talk to it.
type app struct {
	t       *testing.T
	baseURL string
	cmd     *exec.Cmd
	logs    *bytes.Buffer
}

// binaryPath builds the application once for the whole package.
var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gtr-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create a temporary directory:", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(dir, "go-time-recording"+exeSuffix())

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/main.go")
	build.Dir = repoRoot()
	build.Stderr = os.Stderr

	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot build the application:", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}

	return ""
}

// repoRoot is two levels up from test/integration.
func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	return filepath.Join(wd, "..", "..")
}

// start launches a fresh instance with its own database and configuration.
//
// Each test gets its own: they create users, change the instance timezone and
// complete the setup wizard, and sharing one instance would make the outcome
// depend on which test ran first.
func start(t *testing.T, env ...string) *app {
	t.Helper()

	dir := t.TempDir()

	// GoFr resolves ./configs relative to the working directory, so the real
	// configuration is copied beside the binary rather than approximated.
	copyConfigs(t, filepath.Join(repoRoot(), "cmd", "configs"), filepath.Join(dir, "configs"))

	port := freePort(t)

	cmd := exec.Command(binaryPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"APP_ENV=", // only .env, so a stray .local.env cannot change the outcome
		fmt.Sprintf("HTTP_PORT=%d", port),
		fmt.Sprintf("METRICS_PORT=%d", freePort(t)),
		"DB_DIALECT=sqlite",
		"DB_NAME="+filepath.Join(dir, "test"),
		"LOG_LEVEL=WARN",
	)

	// A DSN in the environment points the whole suite at PostgreSQL instead,
	// which is how the same tests verify the dialect used in production.
	if dsn := os.Getenv("GTR_TEST_DSN"); dsn != "" {
		cmd.Env = append(cmd.Env, postgresEnv(t, dsn)...)
	}

	cmd.Env = append(cmd.Env, env...)

	logs := &bytes.Buffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs

	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the application: %v", err)
	}

	a := &app{
		t:       t,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		cmd:     cmd,
		logs:    logs,
	}

	t.Cleanup(a.stop)
	a.waitUntilReady()

	return a
}

// postgresEnv gives this test its own database on the configured server and
// returns the DB_* variables pointing at it.
//
// Its own, not a shared one: these tests change the administrator password,
// create users and complete the setup wizard. Sharing a database would make
// every test after the first sign in with the wrong password, and the outcome
// would depend on which one ran first. The SQLite path gets this for free -
// each instance has its own file - so it has to be arranged here.
func postgresEnv(t *testing.T, dsn string) []string {
	t.Helper()

	// postgres://user:password@host:port/name?sslmode=disable
	trimmed := strings.TrimPrefix(strings.TrimPrefix(dsn, "postgres://"), "postgresql://")

	credentials, rest, ok := strings.Cut(trimmed, "@")
	if !ok {
		t.Fatalf("GTR_TEST_DSN is not a postgres URL: %q", dsn)
	}

	user, password, _ := strings.Cut(credentials, ":")
	hostPort, nameAndQuery, _ := strings.Cut(rest, "/")
	host, port, _ := strings.Cut(hostPort, ":")
	adminDB, _, _ := strings.Cut(nameAndQuery, "?")

	name := createTestDatabase(t, dsn)

	return []string{
		"DB_DIALECT=postgres",
		"DB_HOST=" + host,
		"DB_PORT=" + port,
		"DB_USER=" + user,
		"DB_PASSWORD=" + password,
		"DB_NAME=" + name,
		"DB_SSL_MODE=disable",
		// Unused by the application; kept so a failure message can say which
		// server the throwaway database was created on.
		"GTR_TEST_ADMIN_DB=" + adminDB,
	}
}

// databaseCounter numbers the throwaway databases. Tests can run in parallel,
// so it is incremented atomically.
var databaseCounter atomic.Int64

// createTestDatabase creates an empty database and drops it when the test ends.
func createTestDatabase(t *testing.T, dsn string) string {
	t.Helper()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("cannot reach the PostgreSQL server: %v", err)
	}

	defer func() { _ = db.Close() }()

	// Lower case and no punctuation: an identifier that would need quoting
	// everywhere it appears is a poor choice for a name generated in a test.
	name := fmt.Sprintf("gtr_it_%d_%d", time.Now().UnixNano()%1e9, databaseCounter.Add(1))

	if _, err := db.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("cannot create the test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		cleanup, err := sql.Open("postgres", dsn)
		if err != nil {
			return
		}

		defer func() { _ = cleanup.Close() }()

		// WITH (FORCE) disconnects the application, which may still be shutting
		// down when the test finishes.
		_, _ = cleanup.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})

	return name
}

func freePort(t *testing.T) int {
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
func (a *app) waitUntilReady() {
	a.t.Helper()

	deadline := time.Now().Add(startupTimeout)
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

	a.t.Fatalf("the application did not become ready within %s:\n%s", startupTimeout, a.logs.String())
}

func (a *app) stop() {
	if a.cmd.Process == nil {
		return
	}

	_ = a.cmd.Process.Kill()
	_, _ = a.cmd.Process.Wait()
}

// log returns what the instance has written so far, for a failure message.
func (a *app) log() string {
	return a.logs.String()
}

// ------------------------------------------------------------------ client

// client is one browser session: its own cookie jar, and the CSRF handling a
// browser's script would do.
type client struct {
	t    *testing.T
	app  *app
	http *http.Client
}

// newClient opens a session and fetches the page once, which is what hands out
// the CSRF cookie.
func (a *app) newClient() *client {
	a.t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatalf("cannot create a cookie jar: %v", err)
	}

	c := &client{
		t:    a.t,
		app:  a,
		http: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}

	// The visit a browser makes before anything else.
	c.do(http.MethodGet, "/", nil)

	return c
}

// response is a decoded API answer.
type response struct {
	Status int
	Body   []byte
}

// Data unmarshals GoFr's {data: …} envelope into target.
func (r response) Data(t *testing.T, target any) {
	t.Helper()

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, r.Body)
	}

	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("cannot decode data: %v\n%s", err, envelope.Data)
	}
}

// Message pulls the error text out, for asserting on a refusal.
func (r response) Message() string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	_ = json.Unmarshal(r.Body, &envelope)

	return envelope.Error.Message
}

// csrfToken reads the cookie our own script would read.
func (c *client) csrfToken() string {
	parsed := c.app.baseURL

	u, err := parseURL(parsed)
	if err != nil {
		c.t.Fatalf("bad base URL: %v", err)
	}

	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == "gtr_csrf" {
			return cookie.Value
		}
	}

	return ""
}

// do performs a request the way the interface does: JSON, same-origin, and the
// CSRF token echoed on anything that changes state.
func (c *client) do(method, path string, body any) response {
	c.t.Helper()

	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("cannot encode the request body: %v", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, c.app.baseURL+path, reader)
	if err != nil {
		c.t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.app.baseURL)

	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", c.csrfToken())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s failed: %v\n%s", method, path, err, c.app.log())
	}

	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("cannot read the response: %v", err)
	}

	return response{Status: resp.StatusCode, Body: data}
}

// api is do() with the /api/v1 prefix, which is every call but the assets.
func (c *client) api(method, path string, body any) response {
	c.t.Helper()

	return c.do(method, "/api/v1"+path, body)
}

// must fails the test unless the call returned one of the expected statuses.
func (c *client) must(r response, expected ...int) response {
	c.t.Helper()

	for _, want := range expected {
		if r.Status == want {
			return r
		}
	}

	c.t.Fatalf("expected status %v, got %d: %s", expected, r.Status, r.Body)

	return r
}

// signIn authenticates and returns the signed-in user.
func (c *client) signIn(email, password string) userResponse {
	c.t.Helper()

	r := c.must(c.api(http.MethodPost, "/auth/login",
		map[string]string{"email": email, "password": password}), http.StatusCreated, http.StatusOK)

	var result struct {
		User userResponse `json:"user"`
	}

	r.Data(c.t, &result)

	return result.User
}

// signInAsAdmin signs in and gets the initial-password requirement out of the
// way, which the server otherwise refuses everything else over.
func (a *app) signInAsAdmin(newPassword string) *client {
	a.t.Helper()

	c := a.newClient()
	c.signIn(adminEmail, adminPassword)

	c.must(c.api(http.MethodPut, "/me/password", map[string]string{
		"currentPassword": adminPassword,
		"newPassword":     newPassword,
	}), http.StatusOK)

	// The password change ends every session of that user, so this one is over.
	fresh := a.newClient()
	fresh.signIn(adminEmail, newPassword)

	return fresh
}

// ------------------------------------------------------------------- types

type userResponse struct {
	ID                 uint    `json:"id"`
	Name               string  `json:"name"`
	Email              string  `json:"email"`
	Role               string  `json:"role"`
	IsSystem           bool    `json:"isSystem"`
	IsExternal         bool    `json:"isExternal"`
	MustChangePassword bool    `json:"mustChangePassword"`
	Language           string  `json:"language"`
	Timezone           string  `json:"timezone"`
	EffectiveTimezone  string  `json:"effectiveTimezone"`
	TourSeen           bool    `json:"tourSeen"`
	DailyTargetHours   float64 `json:"dailyTargetHours"`
	MaxDailyHours      float64 `json:"maxDailyHours"`
}

type timesheetResponse struct {
	ID            uint    `json:"id"`
	UserID        uint    `json:"userId"`
	ProjectID     *uint   `json:"projectId"`
	Date          string  `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
}

type projectResponse struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Private bool   `json:"private"`
	OwnerID *uint  `json:"ownerId"`
}

type listOf[T any] struct {
	Items      []T  `json:"items"`
	TotalCount uint `json:"totalCount"`
}

// parseURL is url.Parse, wrapped so the import stays out of the type section.
func parseURL(raw string) (*neturl.URL, error) { return neturl.Parse(raw) }

// path builds a URL path from mixed segments, so a test does not have to
// convert ids by hand at every call site.
func path(parts ...any) string {
	var b strings.Builder

	for _, part := range parts {
		fmt.Fprintf(&b, "%v", part)
	}

	return b.String()
}

// readResponse drains and closes a response into the shape the assertions use.
func readResponse(t *testing.T, resp *http.Response) response {
	t.Helper()

	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("cannot read the response: %v", err)
	}

	return response{Status: resp.StatusCode, Body: data}
}
