//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

// Deleting things that other things point at.
//
// The schema declares foreign keys - user_id REFERENCES users(id) - with no ON
// DELETE clause, so the default is to refuse. Nothing sets PRAGMA foreign_keys
// on SQLite, where enforcement is off unless asked for. That combination means
// the same request can behave differently depending on which database an
// installation chose, which is exactly the kind of difference these tests exist
// to catch: the unit tests use an in-memory repository and cannot see it at all.
//
// What has to be true, whichever dialect is underneath: either the deletion
// takes everything that belonged to the account with it, or it is refused and
// says why. What must not happen is a half-deletion - an account gone with its
// recorded hours still in the reports, or its passkeys still able to sign in.

// countEntriesFor returns how many time entries the database still holds for a user.
//
// Asked of the database, because HTTP cannot answer it. Reading somebody else's
// recorded time is not a right anybody has - not the administrator that does the
// deleting, not a role an installation could define - so an entry left behind by a
// half-deletion is invisible to every account there is. It would sit there pointing
// at an id that no longer exists until it broke a foreign key on another dialect.
//
// This used to be asked over HTTP, as a caller holding timesheets:read:all. That
// right is gone, and the honest replacement is not a different caller: it is to stop
// pretending the API is the right instrument. What cascades is the database.
func countEntriesFor(t *testing.T, a *app, userID uint) int {
	t.Helper()

	return a.Rows(t, "timesheets", "user_id = ?", userID)
}

// createUserWithTime adds an account and has it book an hour.
//
// Two callers, because these are two jobs: the account is the administrator's to
// create, and the hour is nobody's to book but its owner's. So the new account signs
// in and records its own, which is the only shape a real installation ever has.
func createUserWithTime(t *testing.T, a *app, admin *client, email string) uint {
	t.Helper()

	const password = "doomed-password-1"

	var created struct {
		ID uint `json:"id"`
	}

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Doomed", "email": email,
		"role": "user", "password": password,
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	if created.ID == 0 {
		t.Fatal("the new account has no id")
	}

	doomed := a.newClient()
	doomed.signIn(email, password)

	doomed.must(doomed.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-01", "durationHours": 4,
		"description": "work that was really done",
	}), http.StatusCreated, http.StatusOK)

	if got := countEntriesFor(t, a, created.ID); got != 1 {
		t.Fatalf("the entry was not recorded: %d entries for the new account", got)
	}

	return created.ID
}

// An account with recorded time is the normal case for a leaver, so this is the
// path a real deletion actually takes.
func TestDeletingAnAccountThatHasRecordedTime(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	userID := createUserWithTime(t, a, c, "leaver@example.com")

	response := c.api(http.MethodDelete, path("/users/", userID), nil)

	switch response.Status {
	case http.StatusOK, http.StatusNoContent:
		// Accepted. Then nothing of the account may be left pointing at an id
		// that no longer exists - an orphaned entry still counts towards a
		// project report while belonging to nobody.
		if got := countEntriesFor(t, a, userID); got != 0 {
			t.Errorf("the account was deleted but %d of its time entries remain, "+
				"pointing at a user that no longer exists", got)
		}

		if got := c.api(http.MethodGet, path("/users/", userID), nil).Status; got != http.StatusNotFound {
			t.Errorf("reading the deleted account returns %d, want 404", got)
		}

	case http.StatusConflict:
		// Refused. Then the refusal has to explain itself, and the account has
		// to still be usable rather than half-gone.
		if response.Message() == "" {
			t.Error("the deletion was refused without saying why")
		}

		if got := c.api(http.MethodGet, path("/users/", userID), nil).Status; got != http.StatusOK {
			t.Errorf("the deletion was refused but the account reads as %d, want 200", got)
		}

		if got := countEntriesFor(t, a, userID); got != 1 {
			t.Errorf("the deletion was refused but the account has %d entries, want 1", got)
		}

	default:
		t.Errorf("deleting an account with recorded time answered %d: %s\n\n"+
			"Neither an acceptance nor an explained refusal. On a database that "+
			"enforces the foreign key this is the constraint surfacing as an "+
			"internal error.\n\napplication log:\n%s",
			response.Status, response.Body, a.log())
	}
}

// The same for an account with nothing attached, which has to work on every
// dialect - it is the only deletion with no ambiguity about it.
func TestDeletingAnAccountWithNothingAttached(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	var created struct {
		ID uint `json:"id"`
	}

	c.must(c.api(http.MethodPost, "/users", map[string]any{
		"name": "Passing through", "email": "transient@example.com",
		"role": "user", "password": "transient-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	c.must(c.api(http.MethodDelete, path("/users/", created.ID), nil),
		http.StatusOK, http.StatusNoContent)

	if got := c.api(http.MethodGet, path("/users/", created.ID), nil).Status; got != http.StatusNotFound {
		t.Errorf("the deleted account reads as %d, want 404", got)
	}
}

// A role with permissions is two tables. Replacing its permissions deletes them
// all and inserts the new set, so a failure part-way leaves a role that grants
// less than it says - or nothing at all, taking every user on it with it.
func TestChangingARolesPermissionsIsAllOrNothing(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	var role struct {
		ID          uint     `json:"id"`
		Permissions []string `json:"permissions"`
	}

	c.must(c.api(http.MethodPost, "/roles", map[string]any{
		"name": "bookkeeping", "description": "reads its own figures",
		"permissions": []string{"reports:read:own", "timesheets:read:own"},
	}), http.StatusCreated, http.StatusOK).Data(t, &role)

	// A permission that does not exist has to be refused - and refusing after
	// the delete has already happened would leave the role stripped.
	refused := c.api(http.MethodPut, path("/roles/", role.ID), map[string]any{
		"name": "bookkeeping", "description": "reads its own figures",
		"permissions": []string{"reports:read:own", "not:a:permission"},
	})

	if refused.Status == http.StatusOK {
		t.Fatal("a permission this application does not enforce was accepted")
	}

	var after struct {
		Permissions []string `json:"permissions"`
	}

	c.must(c.api(http.MethodGet, path("/roles/", role.ID), nil), http.StatusOK).Data(t, &after)

	if len(after.Permissions) != 2 {
		t.Errorf("the role now holds %v; a refused change left it altered, which is "+
			"how everyone on this role silently loses access", after.Permissions)
	}
}

// The confirmed deletion. This is the path that actually removes things, and the
// one where a table missing from the cascade shows up - as a refusal on a
// database that enforces its foreign keys, or as an orphan on one that does not.
func TestConfirmingTheDeletionRemovesTheAccountAndItsTime(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	userID := createUserWithTime(t, a, c, "confirmed@example.com")

	// Unconfirmed is refused, and says how much is at stake.
	refused := c.api(http.MethodDelete, path("/users/", userID), nil)
	if refused.Status != http.StatusConflict {
		t.Fatalf("the unconfirmed deletion answered %d, want 409: %s", refused.Status, refused.Body)
	}

	if !strings.Contains(refused.Message(), "1") {
		t.Errorf("the refusal does not say how many entries are at stake: %q", refused.Message())
	}

	// Confirmed goes through, on every dialect.
	c.must(c.api(http.MethodDelete, path("/users/", userID, "?purge=true"), nil),
		http.StatusOK, http.StatusNoContent)

	if got := c.api(http.MethodGet, path("/users/", userID), nil).Status; got != http.StatusNotFound {
		t.Errorf("the deleted account reads as %d, want 404", got)
	}

	if got := countEntriesFor(t, a, userID); got != 0 {
		t.Errorf("%d time entries survived the confirmed deletion", got)
	}
}

// An account with nothing recorded needs no confirmation. Asking about something
// with no consequence trains people to click through the dialog that has one.
func TestAnAccountWithNoRecordedTimeNeedsNoConfirmation(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	var created struct {
		ID uint `json:"id"`
	}

	c.must(c.api(http.MethodPost, "/users", map[string]any{
		"name": "Never booked", "email": "nothing@example.com",
		"role": "user", "password": "nothing-password-1",
	}), http.StatusCreated, http.StatusOK).Data(t, &created)

	c.must(c.api(http.MethodDelete, path("/users/", created.ID), nil),
		http.StatusOK, http.StatusNoContent)
}

// The built-in administrator stays, confirmation or not: it is the way back into
// an installation.
func TestTheBuiltInAdministratorCannotBeDeletedEvenWithConfirmation(t *testing.T) {
	t.Parallel()

	a := start(t)
	c := a.signInAsAdmin("a-much-better-password")

	var me struct {
		User struct {
			ID uint `json:"id"`
		} `json:"user"`
	}

	c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if got := c.api(http.MethodDelete, path("/users/", me.User.ID, "?purge=true"), nil).Status; got == http.StatusOK {
		t.Fatal("the built-in administrator was deleted")
	}

	c.must(c.api(http.MethodGet, "/me", nil), http.StatusOK)
}
