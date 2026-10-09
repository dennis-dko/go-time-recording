//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Changing the attribute the directory's identifier is read from forgets the
// identifiers the accounts recorded, and saving the card unchanged does not.
//
// The rule is the settings service's and is held there; what only a real
// database says is whether the one statement that forgets them runs on it.
func TestChangingTheIdentifierAttributeForgetsTheRecordedIdentifiers(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	created := admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Dirk", "email": "dirk@example.com",
		"role": "user", "password": "dirk-password-1",
	})

	var made struct {
		ID uint `json:"id"`
	}

	admin.must(created, http.StatusCreated).Data(t, &made)
	markAsDirectoryAccount(t, a, made.ID)

	recorded := fmt.Sprintf("uid=%d", made.ID)

	admin.must(admin.api(http.MethodPut, "/settings/ldap",
		map[string]any{"idAttribute": "entryUUID"}), http.StatusOK)

	if got := recordedIdentifier(t, a, made.ID); got != recorded {
		t.Errorf("saving the attribute the installation already reads left %q, want %q", got, recorded)
	}

	admin.must(admin.api(http.MethodPut, "/settings/ldap",
		map[string]any{"idAttribute": "objectGUID"}), http.StatusOK)

	if got := recordedIdentifier(t, a, made.ID); got != "" {
		t.Errorf("changing the attribute left the identifier %q recorded under the old one", got)
	}
}

// recordedIdentifier reads the directory identifier an account carries, from the
// database because no answer of the API includes it.
func recordedIdentifier(t *testing.T, a *app, id uint) string {
	t.Helper()

	db := a.DB(t)
	if db == nil {
		t.Fatal("this instance has no database")
	}

	query := "SELECT external_id FROM users WHERE id = ?"
	if strings.Contains(os.Getenv(harness.DSNEnv), "postgres") {
		query = "SELECT external_id FROM users WHERE id = $1"
	}

	var identifier string
	if err := db.QueryRow(query, id).Scan(&identifier); err != nil {
		t.Fatalf("reading the account's identifier: %v", err)
	}

	return identifier
}
