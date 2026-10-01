//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// An account created here and signed in to through the directory is the
// directory's: no demand for a new password, and no local password left.
//
// Measured against the real directory before the fix: an account an
// administrator created without a password, then signed in to with the
// directory's password, came back as local with mustChangePassword set - so its
// owner was sent to replace a password they never had, on every sign-in - and
// the documented initial password, typed as that person, answered 201.
func TestAnAccountCreatedHereIsTheDirectorysOnceItsOwnerSignsInThroughIt(t *testing.T) {
	t.Parallel()

	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	admin.must(admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Bob", "email": "bob@example.com", "role": "user",
	}), http.StatusCreated, http.StatusOK)

	signedIn := a.newClient().signIn("bob", "bob-password")

	if !signedIn.IsExternal {
		t.Error("the account the directory signed bob in to is still a local one")
	}

	if signedIn.MustChangePassword {
		t.Error("bob, signed in with the directory's password, is told to set a new one")
	}

	r := a.newClient().api(http.MethodPost, "/auth/login",
		map[string]string{"email": "bob@example.com", "password": "changeme123"})
	if r.Status != http.StatusUnauthorized && r.Status != http.StatusBadRequest {
		t.Errorf("the documented initial password answered %d for an account the directory owns", r.Status)
	}
}
