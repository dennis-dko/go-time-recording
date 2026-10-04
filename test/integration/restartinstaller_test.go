//go:build integration

package integration

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A restart from the button with no connection left comes back as the installer.
//
// A restart reads the configuration the way a stop and a start do, so a
// connection file that has been removed - deliberately, which is how the manual
// brings the installer back, or by a volume that went missing - takes the
// installation back to its installer when the button is pressed. Whatever waits
// on the other side of the restart has to be ready for that answer rather than
// for the application alone.
func TestARestartWithNoConnectionLeftComesBackAsTheInstaller(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	c := openInstaller(t)

	if status, body := c.post("/install/save", c.token, sqliteDatasource()); status != http.StatusOK {
		t.Fatalf("saving a working connection: got %d, %v", status, body)
	}

	waitForTheApplication(t, c)

	admin := (&app{App: c.app, t: t}).signInAsAdmin("a-much-better-password")

	if err := os.Remove(filepath.Join(c.app.Dir(), "configs", "datasource.json")); err != nil {
		t.Fatalf("removing the connection file: %v", err)
	}

	admin.must(admin.api(http.MethodPost, "/settings/restart", nil),
		http.StatusCreated, http.StatusOK)

	if !eventuallyWithin(45*time.Second, func() bool {
		response, err := http.Get(c.app.BaseURL() + "/install/state")
		if err != nil {
			return false
		}

		defer func() { _ = response.Body.Close() }()

		body, _ := io.ReadAll(response.Body)

		return strings.Contains(response.Header.Get("Content-Type"), "json") &&
			strings.Contains(string(body), "appName")
	}) {
		t.Fatalf("the installer never answered after the restart\n\napplication log:\n%s",
			truncate(c.app.Log(), 2000))
	}
}
