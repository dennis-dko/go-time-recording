//go:build integration

package integration

import (
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
	a := start(t)

	if strings.Contains(a.Log(), "serving its installer") {
		t.Errorf("the installer ran despite a configured database:\n%s", a.Log())
	}

	body := get(t, a.BaseURL()+"/")
	if strings.Contains(body, "Setup token") {
		t.Error("the installer page is being served to a configured installation")
	}
}
