//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/support/security"
	"github.com/dennis-dko/go-time-recording/test/kerberostest"
	"github.com/dennis-dko/go-time-recording/test/tempdir"
)

// kerberosState mirrors the answer to whether a ticket sign-in is on offer.
type kerberosState struct {
	Available bool `json:"available"`
}

// A Kerberos ticket signs its owner in to the account the directory holds, and
// the session it opens is the browser's from then on.
func TestAKerberosTicketSignsItsOwnerIn(t *testing.T) {
	t.Parallel()

	host, port := requireLDAP(t)

	keytab := kerberostest.KeytabFile(t, tempdir.New(t), "the-service-secret")

	a := start(t, "KERBEROS_KEYTAB="+keytab)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	visitor := a.newClient()

	var state kerberosState

	visitor.must(visitor.api(http.MethodGet, "/auth/kerberos", nil), http.StatusOK).Data(t, &state)

	if !state.Available {
		t.Error("with a keytab and a directory, a ticket sign-in is not on offer")
	}

	// Without a ticket the browser is asked for one, which is all a browser that
	// does not trust this address ever sees.
	asked := visitor.api(http.MethodPost, "/auth/kerberos", map[string]any{})
	if asked.Status != http.StatusUnauthorized || asked.Header.Get("WWW-Authenticate") != "Negotiate" {
		t.Errorf("a request without a ticket was answered %d with %q, want 401 asking to negotiate",
			asked.Status, asked.Header.Get("WWW-Authenticate"))
	}

	browser := a.newClient()
	withTicket := browser.withHeader("Authorization",
		kerberostest.Negotiate(t, "the-service-secret", "alice", kerberostest.Realm))

	var login struct {
		User userResponse `json:"user"`
	}

	withTicket.must(withTicket.api(http.MethodPost, "/auth/kerberos", map[string]any{}),
		http.StatusCreated).Data(t, &login)

	if login.User.Email != "alice@example.com" || !login.User.IsExternal {
		t.Errorf("alice's ticket signed in as %+v rather than the directory's alice", login.User)
	}

	var me struct {
		User userResponse `json:"user"`
	}

	browser.must(browser.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	if me.User.Email != "alice@example.com" {
		t.Errorf("the session the ticket opened answers as %q", me.User.Email)
	}

	// A well-sealed ticket for somebody the directory does not hold opens nothing.
	stranger := a.newClient().withHeader("Authorization",
		kerberostest.Negotiate(t, "the-service-secret", "mallory", kerberostest.Realm))

	if r := stranger.api(http.MethodPost, "/auth/kerberos", map[string]any{}); r.Status == http.StatusOK ||
		r.Status == http.StatusCreated {
		t.Errorf("a ticket for somebody the directory does not hold was answered %d", r.Status)
	}

	// And the log says so, since the browser is only shown the form again.
	if !eventually(func() bool {
		return strings.Contains(a.log(), "the directory holds nobody named mallory or mallory@"+kerberostest.Realm)
	}) {
		t.Error("a ticket nobody in the directory owns was refused without a word in the log")
	}
}

// A ticket stands in for the password and for nothing else: an account holding a
// second factor is asked for it, and nothing is opened until it is given.
func TestAKerberosTicketStillAsksForTheSecondFactor(t *testing.T) {
	t.Parallel()

	host, port := requireLDAP(t)

	keytab := kerberostest.KeytabFile(t, tempdir.New(t), "the-service-secret")

	a := start(t, "KERBEROS_KEYTAB="+keytab)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	alice := a.newClient()
	alice.signIn("alice@example.com", "alice-password")

	setup := beginEnrolment(t, alice)

	code, err := security.CurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatal(err)
	}

	alice.must(alice.api(http.MethodPut, "/me/totp", map[string]any{"code": code}), http.StatusOK)

	// A fresh ticket for each request, as a browser presents one: the service
	// refuses an authenticator it has already seen.
	browser := a.newClient()
	ticket := func() *client {
		return browser.withHeader("Authorization",
			kerberostest.Negotiate(t, "the-service-secret", "alice", kerberostest.Realm))
	}

	var asked struct {
		User         *userResponse `json:"user"`
		TOTPRequired bool          `json:"totpRequired"`
	}

	first := ticket()
	first.must(first.api(http.MethodPost, "/auth/kerberos", map[string]any{}), http.StatusCreated).Data(t, &asked)

	if !asked.TOTPRequired || asked.User != nil {
		t.Errorf("a ticket for an account with a second factor was answered %+v, want the code asked for", asked)
	}

	if r := browser.api(http.MethodGet, "/me", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a ticket without its second factor opened a session: /me answered %d", r.Status)
	}

	// The step after the one spent confirming the enrolment, which a code
	// already used would not be.
	next, err := security.TOTPCodeAt(setup.Secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	var login struct {
		User userResponse `json:"user"`
	}

	second := ticket()
	second.must(second.api(http.MethodPost, "/auth/kerberos", map[string]any{"totp": next}),
		http.StatusCreated).Data(t, &login)

	if login.User.Email != "alice@example.com" {
		t.Errorf("the ticket and its second factor signed in as %q", login.User.Email)
	}

	browser.must(browser.api(http.MethodGet, "/me", nil), http.StatusOK)
}

// Without a keytab nothing is on offer, and a request says why; with a keytab
// and no directory there is nobody to look a ticket's name up with.
func TestAKerberosSignInIsOfferedOnlyWhereItCanSucceed(t *testing.T) {
	t.Parallel()

	without := start(t)
	visitor := without.newClient()

	var state kerberosState

	visitor.must(visitor.api(http.MethodGet, "/auth/kerberos", nil), http.StatusOK).Data(t, &state)

	if state.Available {
		t.Error("an installation without a keytab offers a ticket sign-in")
	}

	if r := visitor.api(http.MethodPost, "/auth/kerberos", map[string]any{}); r.Status != http.StatusConflict {
		t.Errorf("a ticket sign-in without a keytab was answered %d, want 409", r.Status)
	}

	keytab := kerberostest.KeytabFile(t, tempdir.New(t), "the-service-secret")
	noDirectory := start(t, "KERBEROS_KEYTAB="+keytab)

	other := noDirectory.newClient()
	other.must(other.api(http.MethodGet, "/auth/kerberos", nil), http.StatusOK).Data(t, &state)

	if state.Available {
		t.Error("a keytab without a directory offers a ticket sign-in nobody can complete")
	}

	withTicket := other.withHeader("Authorization",
		kerberostest.Negotiate(t, "the-service-secret", "alice", kerberostest.Realm))

	if r := withTicket.api(http.MethodPost, "/auth/kerberos", map[string]any{}); r.Status != http.StatusConflict {
		t.Errorf("a ticket without a directory to look it up in was answered %d, want 409", r.Status)
	}
}

// The start says what a keytab without a directory amounts to: a ticket sign-in
// offered to nobody, rather than one that is on.
//
// The line was written the moment the keytab was read, before anybody had
// looked for a directory, and the operations guide quotes it as the sign that
// everything worked - while saying in the same section that a keytab without a
// directory offers nothing. At INFO, so a wrong "on" would be in the log.
func TestTheStartDoesNotCallATicketSignInOnWithoutADirectory(t *testing.T) {
	t.Parallel()

	keytab := kerberostest.KeytabFile(t, tempdir.New(t), "the-service-secret")
	a := start(t, "KERBEROS_KEYTAB="+keytab, "LOG_LEVEL=INFO")

	said := ""

	told := eventually(func() bool {
		said = a.log()

		return strings.Contains(said, "offered to nobody")
	})

	if strings.Contains(said, "signing in with a Kerberos ticket of realm "+kerberostest.Realm+" is on") {
		t.Error("the start called the ticket sign-in on with no directory to look a ticket's owner up in")
	}

	if !told {
		t.Error("the start did not say that a keytab without a directory offers the ticket sign-in to nobody")
	}
}
