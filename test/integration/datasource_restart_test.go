//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// What the database settings say about a restart is what the restart card says.
//
// The card compares the connection on disk with the one this process opened,
// and it is what the screen asks. The two answers an API client reads were
// written before it and never brought into step: the save answered that a
// restart is required every time, also for the running connection saved
// unchanged, and the settings themselves carried the same field with nothing
// ever setting it, so it was false while a changed connection sat waiting. The telemetry
// settings beside them were corrected to ask the card's rule; these were the
// second place that needed it.
func TestTheDatabaseSettingsAgreeWithTheRestartCard(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	type settings struct {
		Running struct {
			Dialect string `json:"dialect"`
			Name    string `json:"name"`
			Host    string `json:"host"`
			Port    string `json:"port"`
			User    string `json:"user"`
			SSLMode string `json:"sslMode"`
		} `json:"running"`
		RestartRequired bool `json:"restartRequired"`
	}

	read := func() settings {
		t.Helper()

		var s settings

		admin.must(admin.api(http.MethodGet, "/settings/datasource", nil), http.StatusOK).Data(t, &s)

		return s
	}

	save := func(name string) bool {
		t.Helper()

		running := read().Running

		var saved struct {
			RestartRequired bool `json:"restartRequired"`
		}

		// The password left out, which keeps the one in force for the same server
		// and user.
		admin.must(admin.api(http.MethodPut, "/settings/datasource", map[string]any{
			"dialect": running.Dialect, "name": name, "host": running.Host,
			"port": running.Port, "user": running.User, "sslMode": running.SSLMode,
		}), http.StatusOK).Data(t, &saved)

		return saved.RestartRequired
	}

	if read().RestartRequired {
		t.Error("an installation nobody has touched says its database is waiting for a restart")
	}

	running := read().Running.Name

	if save(running) {
		t.Error("saving the running connection unchanged answered that a restart is required")
	}

	if read().RestartRequired || len(restartState(t, admin).Pending) != 0 {
		t.Error("after saving the running connection unchanged, something is said to be waiting")
	}

	if !save(running + "-moved") {
		t.Error("saving another database answered that no restart is required")
	}

	if !read().RestartRequired {
		t.Error("another database is saved and the settings say nothing is waiting for a restart")
	}

	if _, _, found := restartState(t, admin).pendingFor("database"); !found {
		t.Error("another database is saved and the restart card does not list it")
	}
}
