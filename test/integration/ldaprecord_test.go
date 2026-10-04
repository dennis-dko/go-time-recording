//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// saysItRemoved reports whether the process log holds the line a run writes for
// an account it deleted.
//
// Two substrings on one line rather than the sentence: the log is JSON, so the
// quotes around the address arrive escaped.
func saysItRemoved(log, email string) bool {
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, "directory sync removed") && strings.Contains(line, email) {
			return true
		}
	}

	return false
}

// A run started from the screen leaves a line naming each account it removed.
//
// That line is the only record there is. The account is gone, and with it every
// row that named it - the entries, the projects, the sessions - so afterwards
// nothing in the database says there was ever such a person. The handler wrote
// the line and nothing checked that it did.
func TestADirectoryRunFromTheScreenSaysWhomItRemoved(t *testing.T) {
	t.Parallel()

	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	// Everybody the directory holds, as accounts here.
	admin.must(admin.api(http.MethodPost, "/settings/ldap/sync", nil),
		http.StatusCreated, http.StatusOK)

	// And then a directory that no longer holds the contractor.
	configureLDAP(t, admin, host, port, ldapPeopleDN)

	var ran SyncPreview

	admin.must(admin.api(http.MethodPost, "/settings/ldap/sync", nil),
		http.StatusCreated, http.StatusOK).Data(t, &ran)

	if ran.Aborted != "" || len(ran.Deleted) != 1 {
		t.Fatalf("the run was meant to remove the one contractor and answered %+v", ran)
	}

	if !eventually(func() bool { return saysItRemoved(a.log(), "dave@example.com") }) {
		t.Errorf("the run removed dave@example.com and the log does not say so:\n%s",
			truncate(a.log(), 3000))
	}

	if saysItRemoved(a.log(), "alice@example.com") {
		t.Error("the log says the run removed somebody the directory still holds")
	}
}

// A scheduled run does what a run from the screen does, and says so the same way.
//
// The schedule is the one path nobody watches: it runs at night, deletes whoever
// the directory has let go together with their hours, and has nobody to show a
// report to. Whether a job was registered was checked; whether one ever ran,
// removed anybody or left a line about it was not - and a job that never runs
// looks exactly like a directory that has not changed.
//
// Every minute, the shortest a five-field expression can say, so this waits for
// the next minute to turn.
func TestAScheduledDirectoryRunRemovesWhoHasLeftAndSaysSo(t *testing.T) {
	t.Parallel()

	host, port := requireLDAP(t)

	a := start(t, "LDAP_SYNC_SCHEDULE=* * * * *")
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	// Three people the directory holds, each an account here by signing in.
	for _, who := range []struct{ login, password string }{
		{"alice@example.com", "alice-password"},
		{"bob@example.com", "bob-password"},
		{"dave@example.com", "dave-password"},
	} {
		arriving := a.newClient()

		arriving.must(arriving.api(http.MethodPost, "/auth/login", map[string]string{
			"email": who.login, "password": who.password,
		}), http.StatusCreated, http.StatusOK)
	}

	// The contractor leaves the part of the directory this installation reads.
	configureLDAP(t, admin, host, port, ldapPeopleDN)

	if !eventuallyWithin(90*time.Second, func() bool {
		return saysItRemoved(a.log(), "dave@example.com")
	}) {
		t.Fatalf("no scheduled run removed dave@example.com and said so within a minute and a half:\n%s",
			truncate(a.log(), 3000))
	}

	var accounts listOf[struct {
		Email string `json:"email"`
	}]

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &accounts)

	for _, account := range accounts.Items {
		if account.Email == "dave@example.com" {
			t.Error("the log says the scheduled run removed dave@example.com, and the account is still here")
		}
	}

	if saysItRemoved(a.log(), "alice@example.com") || saysItRemoved(a.log(), "bob@example.com") {
		t.Error("the scheduled run says it removed somebody the directory still holds")
	}
}
