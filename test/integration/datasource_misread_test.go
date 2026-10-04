//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// The settings card refuses a connection the driver would read otherwise than
// it was typed, on save as on test, and writes nothing for the next start.
//
// The card saves without probing - the connection takes effect at the next
// start - so a PostgreSQL connection without a password was written as it stood,
// and that start opened the database named after the account rather than the one
// on the card. The probe at the start agreed with GoFr, because both read the
// same string the same way: measured against a server that asks for no password,
// the tables went into "postgres".
func TestTheSettingsCardRefusesAConnectionTheDriverWouldReadOtherwise(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// Nothing listens on port 1, so a probe let through would fail at once.
	connection := map[string]any{
		"dialect": "postgres", "name": "gtr", "host": "127.0.0.1", "port": "1",
		"user": "postgres", "password": "", "sslMode": "disable",
	}

	saved := admin.api(http.MethodPut, "/settings/datasource", connection)

	if got := refusalOf(t, saved); saved.Status != http.StatusBadRequest || got.Code != "passwordSwallowsName" {
		t.Errorf("saving answered %d %q, want %d passwordSwallowsName: %s",
			saved.Status, got.Code, http.StatusBadRequest, saved.Body)
	}

	var probed struct {
		OK    bool    `json:"ok"`
		Error refusal `json:"error"`
	}

	admin.must(admin.api(http.MethodPost, "/settings/datasource/test", connection),
		http.StatusOK, http.StatusCreated).Data(t, &probed)

	if probed.OK || probed.Error.Code != "passwordSwallowsName" {
		t.Errorf("testing answered ok=%v %q, want the refusal passwordSwallowsName: %s",
			probed.OK, probed.Error.Code, probed.Error.Message)
	}

	var shown struct {
		Stored bool `json:"stored"`
	}

	admin.must(admin.api(http.MethodGet, "/settings/datasource", nil), http.StatusOK).Data(t, &shown)

	if shown.Stored {
		t.Error("the refused connection was written for the next start to open")
	}
}
