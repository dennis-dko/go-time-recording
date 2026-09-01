//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// The installer is the one part of this application that runs without a
// database, which makes it the one part no other test can reach: every other
// test is handed a database on purpose.
//
// What has to hold is narrow and important. A binary with nothing configured
// must not quietly invent a database, must not let an unauthenticated stranger
// choose one, must refuse a connection that does not work, and having accepted
// one must become the actual application on the same port without being
// restarted.

// tokenPattern finds the token the process logs. Read from the log rather than
// set through SETUP_TOKEN, because reading it is what an operator does and the
// message being findable is part of what is under test.
var tokenPattern = regexp.MustCompile(`setup token: ([0-9a-f]{32})`)

// installerClient talks to the installer, which has its own tiny API - no
// session, no CSRF, a token header instead.
type installerClient struct {
	t     *testing.T
	app   *harness.App
	token string
}

func openInstaller(t *testing.T) *installerClient {
	t.Helper()

	app := harness.StartUnconfigured(t)

	// The token is logged during start-up, and waitUntilReady already waited for
	// the port to answer - but the log is written by a different goroutine, so
	// give it a moment rather than assuming an ordering.
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		if match := tokenPattern.FindStringSubmatch(app.Log()); match != nil {
			return &installerClient{t: t, app: app, token: match[1]}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("the installer never logged a setup token:\n%s", app.Log())

	return nil
}

// post calls one of the installer's endpoints and returns the status and body.
func (c *installerClient) post(path, token string, body any) (int, map[string]any) {
	c.t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		c.t.Fatalf("cannot encode the request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.app.BaseURL()+path, strings.NewReader(string(raw)))
	if err != nil {
		c.t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Setup-Token", token)

	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		c.t.Fatalf("%s: %v", path, err)
	}

	defer func() { _ = res.Body.Close() }()

	var decoded map[string]any
	_ = json.NewDecoder(res.Body).Decode(&decoded)

	return res.StatusCode, decoded
}

// sqlite is a connection this test can prove works. The name is relative, so it
// lands in the instance's working directory the way the application's own
// default does.
func sqliteDatasource() map[string]string {
	return map[string]string{"dialect": "sqlite", "name": "chosen"}
}

// An installation with no database configured must serve the installer rather
// than the application - and must not have picked a database for itself, which
// is the failure this whole design exists to prevent.
func TestABinaryWithNoDatabaseServesItsInstaller(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	body := get(t, c.app.BaseURL()+"/")
	if !strings.Contains(body, "Setup token") {
		t.Errorf("expected the installer page, got:\n%s", truncate(body, 400))
	}

	// A deep link into the application has to land somewhere that explains
	// itself rather than on a 404.
	if deep := get(t, c.app.BaseURL()+"/some/bookmarked/path"); !strings.Contains(deep, "Setup token") {
		t.Error("a deep link should also reach the installer")
	}

	// Nothing may have been written yet.
	if _, err := os.Stat(filepath.Join(c.app.Dir(), "configs", "datasource.json")); err == nil {
		t.Error("the installer wrote a connection before anyone chose one")
	}
}

// Until a database exists there is no account to authenticate against, so
// whoever reaches this screen decides where the installation keeps its data.
// The token is the only thing standing between an exposed port and that
// decision.
func TestTheInstallerRefusesAWrongToken(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	for _, token := range []string{"", "wrong", strings.Repeat("a", 32)} {
		status, _ := c.post("/install/save", token, sqliteDatasource())
		if status != http.StatusUnauthorized {
			t.Errorf("token %q: got %d, want 401", token, status)
		}
	}

	if _, err := os.Stat(filepath.Join(c.app.Dir(), "configs", "datasource.json")); err == nil {
		t.Error("a rejected request still wrote a connection")
	}
}

// A saved connection that does not work would leave the process unable to start
// and unable to serve the screen that could fix it. So it is proven first.
func TestTheInstallerRefusesAConnectionThatDoesNotWork(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	unreachable := map[string]string{
		"dialect": "postgres", "name": "nothing",
		// Port 1 is reserved and nothing listens on it.
		"host": "127.0.0.1", "port": "1", "user": "someone",
	}

	status, body := c.post("/install/save", c.token, unreachable)
	if status != http.StatusBadRequest {
		t.Errorf("got %d, want 400", status)
	}

	if message, _ := body["error"].(string); message == "" {
		t.Error("the refusal should say why")
	}

	if _, err := os.Stat(filepath.Join(c.app.Dir(), "configs", "datasource.json")); err == nil {
		t.Error("an unreachable connection was saved anyway")
	}
}

// Nonsense is refused before it is probed, so a missing host is a sentence
// rather than a driver error.
func TestTheInstallerRefusesAnIncompleteConnection(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	for _, ds := range []map[string]string{
		{"dialect": "sqlite"},                // no name
		{"dialect": "postgres", "name": "x"}, // no host, no user
		{"dialect": "oracle", "name": "x"},   // not a dialect this supports
	} {
		status, body := c.post("/install/test", c.token, ds)
		if status != http.StatusBadRequest {
			t.Errorf("%v: got %d, want 400", ds, status)

			continue
		}

		if message, _ := body["error"].(string); message == "" {
			t.Errorf("%v: the refusal should say why", ds)
		}
	}
}

// The whole point: having accepted a connection, the process becomes the
// application on the same port. No restart, because a container that exits to
// finish installing itself looks like one that crashed.
func TestChoosingADatabaseHandsOverToTheApplication(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	if status, body := c.post("/install/test", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("testing a working connection: got %d, %v", status, body)
	}

	if status, body := c.post("/install/save", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("saving a working connection: got %d, %v", status, body)
	}

	// The file the next start reads.
	saved := filepath.Join(c.app.Dir(), "configs", "datasource.json")

	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the connection was not written to %s: %v", saved, err)
	}

	if !strings.Contains(string(raw), `"sqlite"`) {
		t.Errorf("%s does not hold the chosen connection:\n%s", saved, raw)
	}

	// And now the application, on the same port, in the same process.
	deadline := time.Now().Add(harness.StartupTimeout)

	for time.Now().Before(deadline) {
		if body := tryGet(c.app.BaseURL() + "/api/v1/branding"); strings.Contains(body, `"title"`) {
			// Signing in proves the database was really provisioned rather than
			// the port merely being answered.
			client := (&app{App: c.app, t: t}).newClient()
			client.signIn(adminEmail, adminPassword)

			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("the application never took over the port:\n%s", c.app.Log())
}

// An operator who configured the database in a compose file must not be shown
// an installer - that would turn a working deployment into one that waits for
// somebody to click something.
func TestAConfiguredDatabaseSkipsTheInstallerEntirely(t *testing.T) {
	t.Parallel()

	a := start(t)

	if strings.Contains(a.Log(), "serving its installer") {
		t.Errorf("the installer ran despite a configured database:\n%s", a.Log())
	}

	body := get(t, a.BaseURL()+"/")
	if strings.Contains(body, "Setup token") {
		t.Error("the installer page is being served to a configured installation")
	}
}

// claim asks the application, once it is up, for the session the installer earned.
func claim(t *testing.T, c *client, token string) response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.app.BaseURL()+"/api/v1/setup/claim", nil)
	if err != nil {
		t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Origin", c.app.BaseURL())
	req.Header.Set("X-CSRF-Token", c.csrfToken())
	req.Header.Set("X-Setup-Token", token)

	return c.send(req, http.MethodPost, "/api/v1/setup/claim")
}

// waitForTheApplication blocks until the process that was serving an installer
// is serving the application instead.
func waitForTheApplication(t *testing.T, c *installerClient) {
	t.Helper()

	deadline := time.Now().Add(harness.StartupTimeout)

	for time.Now().Before(deadline) {
		if theApplicationAnswered(tryGet(c.app.BaseURL() + "/api/v1/branding")) {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("the application never took over the port:\n%s", c.app.Log())
}

// theApplicationAnswered reports whether that body came from the application
// rather than from the installer still standing on the port.
//
// It has to be told apart, and a substring could not do it. The installer mounts
// its page at "/" and therefore answers *every* path, including the one this
// polls - and its markup carries `data-i18n="title"`, which is the exact string
// the old check searched for. So the wait could be satisfied by the installer,
// return while the handover had not begun, and hand the caller a port that was
// about to close: the installer stops, and TestDatasource, ApplyDatasource,
// gofr.New and the migrations all run before anything binds it again. The next
// request in the test then met "connection refused".
//
// It is rare because the installer has usually stopped answering by the first
// poll, 250ms after the save. It is not rare enough: it took CI on main red
// once, which skipped the release, since Release triggers on CI completing.
//
// Decoded rather than searched. The application answers this route with JSON and
// the installer answers it with HTML, so parsing is the difference itself rather
// than a proxy for it, and a title that happens to appear in future markup cannot
// bring the confusion back.
func theApplicationAnswered(body string) bool {
	// Through GoFr's envelope, which is where the answer actually is. Decoding
	// the bare object instead parses this perfectly well and finds no title, so
	// the wait never returns at all - which is how the first attempt at this fix
	// turned one occasional flake into three reliable timeouts.
	var answer struct {
		Data struct {
			Title *string `json:"title"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		return false
	}

	return answer.Data.Title != nil
}

// Answering the installer is the sign-in.
//
// Whoever completes it has proved more than a password does: the setup token is
// printed to this process's log, so they can already read the process - and what
// they just decided is where every account, project and hour will be kept. Making
// them then find the documented initial password and type it into a sign-in form
// is a step that establishes nothing, and it is the step that puts "changeme123"
// in front of somebody as a thing to remember.
//
// So the installer hands the browser a session for the built-in administrator,
// and the wizard's first screen - choose a password - is what they actually
// arrive at.
func TestTheInstallerHandsTheBrowserASignedInSession(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	if status, body := c.post("/install/save", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("saving a working connection: got %d, %v", status, body)
	}

	waitForTheApplication(t, c)

	browser := (&app{App: c.app, t: t}).newClient()

	if got := browser.api(http.MethodGet, "/me", nil).Status; got == http.StatusOK {
		t.Fatal("this client is signed in before it claimed anything, so nothing " +
			"below would prove the claim did it")
	}

	// 200 or 201: GoFr answers a POST that returns a body with Created, which is
	// what every other creating route here answers too.
	if got := claim(t, browser, c.token); got.Status != http.StatusOK &&
		got.Status != http.StatusCreated {
		t.Fatalf("the installer's own token did not open a session: %d %s",
			got.Status, got.Body)
	}

	var me struct {
		User struct {
			Email              string `json:"email"`
			IsSystem           bool   `json:"isSystem"`
			MustChangePassword bool   `json:"mustChangePassword"`
		} `json:"user"`
	}

	browser.must(browser.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.Email != adminEmail || !me.User.IsSystem {
		t.Errorf("the session belongs to %q (system: %v), not to the built-in "+
			"administrator", me.User.Email, me.User.IsSystem)
	}

	// Still the first thing it has to do. The session is a way past typing a
	// documented password, not a way past choosing a real one.
	if !me.User.MustChangePassword {
		t.Error("the handed-over session is not asked to choose a password, so the " +
			"installation stays reachable on the documented one")
	}

	if got := browser.api(http.MethodGet, "/users", nil).Status; got != http.StatusConflict {
		t.Errorf("the handed-over session reads accounts with status %d; everything "+
			"but the password change has to stay shut until one is chosen", got)
	}
}

// A wrong token opens nothing.
func TestTheHandedOverSessionNeedsTheInstallersOwnToken(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	if status, body := c.post("/install/save", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("saving a working connection: got %d, %v", status, body)
	}

	waitForTheApplication(t, c)

	browser := (&app{App: c.app, t: t}).newClient()

	for _, wrong := range []string{"", "0123456789abcdef0123456789abcdef"} {
		if got := claim(t, browser, wrong); got.Status == http.StatusOK ||
			got.Status == http.StatusCreated {
			t.Errorf("the token %q opened a session", wrong)
		}
	}

	if got := browser.api(http.MethodGet, "/me", nil).Status; got == http.StatusOK {
		t.Error("a refused claim left a session behind anyway")
	}
}

// An installation that never ran an installer hands out nothing at all.
//
// The case that matters, and the one an empty-string comparison would get
// wrong: a deployment configured from a compose file has no setup token, and a
// request arriving with no token header must not match it.
func TestAnInstallationThatSkippedTheInstallerHandsOutNoSession(t *testing.T) {
	t.Parallel()

	a := start(t)
	browser := a.newClient()

	for _, attempt := range []string{"", "0123456789abcdef0123456789abcdef"} {
		if got := claim(t, browser, attempt); got.Status == http.StatusOK ||
			got.Status == http.StatusCreated {
			t.Errorf("an installation with no installer opened a session for token %q",
				attempt)
		}
	}

	if got := browser.api(http.MethodGet, "/me", nil).Status; got == http.StatusOK {
		t.Error("a refused claim left a session behind anyway")
	}
}

// The wait for the application is not satisfied by the installer.
//
// waitForTheApplication is what every handover case leans on, so a wait that can
// be satisfied early makes all of them flaky at once - and it was: the installer
// answers every path with its own page, and that page carried the exact string
// the wait searched for.
//
// Deterministic, where the failure it fixes is a race. Before anything is saved
// the application cannot possibly be up, so whatever answers here is the
// installer by definition, and the wait must not accept it.
func TestTheWaitForTheApplicationIsNotSatisfiedByTheInstaller(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	body := tryGet(c.app.BaseURL() + "/api/v1/branding")
	if body == "" {
		t.Fatal("the installer did not answer the path the wait polls; if it has " +
			"stopped answering every path, this case is no longer about anything")
	}

	if theApplicationAnswered(body) {
		t.Errorf("the wait accepts the installer's own page as the application, so "+
			"it returns before the handover has begun and hands back a port that is "+
			"about to close. The installer answered:\n%.200s", body)
	}
}

// And the door closes the moment the installation is taken into use.
//
// The claim is only ever a substitute for the documented password, so it lasts
// exactly as long as that password does: once a real one has been chosen, the
// token is no longer a way in, whoever still has a copy of the log.
func TestTheHandOverStopsOnceAPasswordHasBeenChosen(t *testing.T) {
	t.Parallel()

	c := openInstaller(t)

	if status, body := c.post("/install/save", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("saving a working connection: got %d, %v", status, body)
	}

	waitForTheApplication(t, c)

	instance := &app{App: c.app, t: t}

	// Taken into use the ordinary way, which is what signInAsAdmin does: sign in
	// on the documented password and replace it.
	instance.signInAsAdmin("a-much-better-password")

	browser := instance.newClient()

	if got := claim(t, browser, c.token); got.Status == http.StatusOK ||
		got.Status == http.StatusCreated {
		t.Error("the setup token still opened a session after a password was chosen")
	}
}
