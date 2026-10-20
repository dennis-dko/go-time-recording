//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/oidctest"
)

type openIDSettings struct {
	Enabled     bool   `json:"enabled"`
	Issuer      string `json:"issuer"`
	ClientID    string `json:"clientId"`
	HasSecret   bool   `json:"hasSecret"`
	RedirectURI string `json:"redirectUri"`
}

// configureProvider has the administrator point the installation at a provider.
func configureProvider(admin *client, p *oidctest.Provider) openIDSettings {
	admin.t.Helper()

	var saved openIDSettings

	admin.must(admin.api(http.MethodPut, "/settings/openid", map[string]any{
		"enabled": true, "issuer": p.URL, "clientId": oidctest.ClientID,
		"clientSecret": oidctest.ClientSecret, "label": "Test provider",
	}), http.StatusOK).Data(admin.t, &saved)

	return saved
}

// roundTrip begins a round as the client, goes through the provider as a browser
// would, and answers with where the provider sent it back.
func roundTrip(c *client, path string) *url.URL {
	c.t.Helper()

	var begun struct {
		Address string `json:"address"`
	}

	c.must(c.api(http.MethodPost, path, nil), http.StatusCreated, http.StatusOK).Data(c.t, &begun)

	return oidctest.Follow(c.t, begun.Address)
}

// finish hands the code and the state a client came back with to the application.
func finish(c *client, back *url.URL) response {
	c.t.Helper()

	return c.api(http.MethodPost, "/auth/openid/finish", map[string]string{
		"state": back.Query().Get("state"), "code": back.Query().Get("code"),
	})
}

// Somebody links their account to their identity at the provider while signed in,
// and from then on that identity signs the account in from a browser that has no
// session - on every database.
func TestALinkedProviderIdentitySignsItsAccountIn(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)
	p := oidctest.Start(t)

	saved := configureProvider(admin, p)

	if !saved.Enabled || !saved.HasSecret || saved.Issuer != p.URL ||
		!strings.HasSuffix(saved.RedirectURI, "/sign-in/openid") {
		t.Fatalf("the provider was stored as %+v", saved)
	}

	if raw := string(admin.api(http.MethodGet, "/settings/openid", nil).Body); strings.Contains(raw, oidctest.ClientSecret) {
		t.Errorf("the client secret is sent back: %s", raw)
	}

	var offer struct {
		Offered bool   `json:"offered"`
		Label   string `json:"label"`
	}

	a.newClient().must(a.newClient().api(http.MethodGet, "/auth/openid", nil), http.StatusOK).Data(t, &offer)

	if !offer.Offered || offer.Label != "Test provider" {
		t.Errorf("the sign-in form is offered %+v", offer)
	}

	back := roundTrip(worker, "/me/openid")

	if back.Path != "/sign-in/openid" || !strings.HasPrefix(a.BaseURL(), back.Scheme+"://"+back.Host) {
		t.Fatalf("the provider sent the browser back to %s, want this installation's /sign-in/openid", back)
	}

	var linked struct {
		Linked bool   `json:"linked"`
		Issuer string `json:"issuer"`
	}

	worker.must(finish(worker, back), http.StatusCreated, http.StatusOK).Data(t, &linked)

	if !linked.Linked || linked.Issuer != p.URL {
		t.Fatalf("the round answered %+v, want the account linked", linked)
	}

	browser := a.newClient()

	var signedIn struct {
		User userResponse `json:"user"`
	}

	browser.must(finish(browser, roundTrip(browser, "/auth/openid")), http.StatusCreated, http.StatusOK).
		Data(t, &signedIn)

	var me struct {
		User userResponse `json:"user"`
	}

	browser.must(browser.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if signedIn.User.Email != "wera@example.com" || me.User.Email != "wera@example.com" {
		t.Errorf("the provider signed in %q and the session is %q, want Wera's account",
			signedIn.User.Email, me.User.Email)
	}

	worker.must(worker.api(http.MethodDelete, "/me/openid", nil), http.StatusOK, http.StatusNoContent)

	after := a.newClient()

	if refused := finish(after, roundTrip(after, "/auth/openid")); refused.Status != http.StatusConflict ||
		!strings.Contains(string(refused.Body), "openidNotLinked") {
		t.Errorf("after unlinking the provider's sign-in answered %d: %s", refused.Status, refused.Body)
	}
}

// A code the provider sent one browser back with cannot be finished by another:
// otherwise whoever began a round could have somebody else's browser signed in as
// them. The browser that began it still can.
func TestARoundIsFinishedOnlyByTheBrowserThatBeganIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)
	p := oidctest.Start(t)

	configureProvider(admin, p)
	worker.must(finish(worker, roundTrip(worker, "/me/openid")), http.StatusCreated, http.StatusOK)

	attacker := a.newClient()
	back := roundTrip(attacker, "/auth/openid")

	victim := a.newClient()

	if refused := finish(victim, back); refused.Status != http.StatusBadRequest ||
		!strings.Contains(string(refused.Body), "attemptExpired") {
		t.Fatalf("another browser finishing the round answered %d: %s", refused.Status, refused.Body)
	}

	if got := victim.api(http.MethodGet, "/me", nil).Status; got != http.StatusUnauthorized {
		t.Errorf("the other browser has a session after all (%d)", got)
	}

	attacker.must(finish(attacker, back), http.StatusCreated, http.StatusOK)
}

// An account's link goes with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsProviderLinkWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)
	p := oidctest.Start(t)

	configureProvider(admin, p)
	worker.must(finish(worker, roundTrip(worker, "/me/openid")), http.StatusCreated, http.StatusOK)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "openid_identities", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d link(s) outlived the account they belonged to", left)
	}
}

// A provider reached over plain HTTP anywhere but this machine is refused, because
// anybody on the way could answer for it and sign anybody in as anybody.
func TestAProviderOverPlainHTTPIsRefused(t *testing.T) {
	t.Parallel()

	_, admin, _ := startWithWorker(t)

	refused := admin.api(http.MethodPut, "/settings/openid", map[string]any{
		"enabled": true, "issuer": "http://id.example.com/realms/acme", "clientId": "gtr",
	})

	if refused.Status != http.StatusBadRequest || !strings.Contains(string(refused.Body), "openidNeedsHttps") {
		t.Errorf("a provider over plain HTTP answered %d: %s", refused.Status, refused.Body)
	}
}
