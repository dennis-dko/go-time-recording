//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// An account the directory holds cannot register a passkey.
//
// Its password is the directory's, and so is the decision that it may still sign
// in: every password sign-in asks the directory, and removing or locking the
// entry there is how an organisation ends somebody's access. A passkey asks
// nobody. Registered to a directory account, it went on opening sessions for a
// person the directory had already let go - until a synchronisation deleted the
// account, and a synchronisation by default never runs. The operations manual
// already said a directory account cannot hold a passkey; nothing refused one.
//
// The account is marked as the directory's after signing in, which is the one
// way to hold a session for it without a directory: a directory account cannot
// sign in with a local password at all.
func TestADirectoryAccountCannotRegisterAPasskey(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	wera := a.signInAsUser(admin, "Wera", "wera@example.com")

	var me struct {
		User struct {
			ID uint `json:"id"`
		} `json:"user"`
	}

	wera.must(wera.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	markAsDirectoryAccount(t, a, me.User.ID)

	res := wera.api(http.MethodPost, "/me/passkeys/register", map[string]any{"name": "phone"})

	if res.Status != http.StatusConflict {
		t.Fatalf("a directory account asked to register a passkey got %d, want %d",
			res.Status, http.StatusConflict)
	}

	if code := errorCode(t, res); code != "directoryAccountHasNoPasskey" {
		t.Errorf("the refusal is coded %q, want directoryAccountHasNoPasskey", code)
	}
}
