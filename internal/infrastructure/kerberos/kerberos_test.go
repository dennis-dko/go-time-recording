package kerberos

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/iana/etypeID"
	"github.com/jcmturner/gokrb5/v8/iana/nametype"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
)

const (
	spn       = "HTTP/zeit.example.com"
	testRealm = "EXAMPLE.COM"
)

// serviceKeytab is a keytab holding this service's key, derived from password.
func serviceKeytab(t *testing.T, password string) *keytab.Keytab {
	t.Helper()

	kt := keytab.New()
	if err := kt.AddEntry(spn, testRealm, password, time.Now(), 1, etypeID.AES256_CTS_HMAC_SHA1_96); err != nil {
		t.Fatalf("building the keytab: %v", err)
	}

	return kt
}

// writtenKeytab is serviceKeytab on disk, as an operator would place it.
func writtenKeytab(t *testing.T, password string) string {
	t.Helper()

	raw, err := serviceKeytab(t, password).Marshal()
	if err != nil {
		t.Fatalf("writing the keytab: %v", err)
	}

	path := filepath.Join(t.TempDir(), "service.keytab")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// negotiate is the header a browser sends: a ticket for this service, issued to
// name of clientRealm and sealed with the key the keytab of servicePassword holds
// - what a KDC does, done here so no KDC is needed.
func negotiate(t *testing.T, servicePassword, name, clientRealm string) string {
	t.Helper()

	now := time.Now().UTC()

	ticket, sessionKey, err := messages.NewTicket(
		types.NewPrincipalName(nametype.KRB_NT_PRINCIPAL, name), clientRealm,
		types.NewPrincipalName(nametype.KRB_NT_SRV_INST, spn), testRealm,
		types.NewKrbFlags(), serviceKeytab(t, servicePassword), etypeID.AES256_CTS_HMAC_SHA1_96, 1,
		now, now, now.Add(time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issuing the ticket: %v", err)
	}

	cl := client.NewWithPassword(name, clientRealm, "unused", config.New(), client.DisablePAFXFAST(true))

	init, err := spnego.NewNegTokenInitKRB5(cl, ticket, sessionKey)
	if err != nil {
		t.Fatalf("building the token: %v", err)
	}

	token := spnego.SPNEGOToken{Init: true, NegTokenInit: init}

	raw, err := token.Marshal()
	if err != nil {
		t.Fatalf("encoding the token: %v", err)
	}

	return "Negotiate " + base64.StdEncoding.EncodeToString(raw)
}

// served sends header to the acceptor and says what it answered and whom next
// was told about.
func served(t *testing.T, acceptor *Acceptor, header string) (int, http.Header, *Principal) {
	t.Helper()

	var seen *Principal

	handler := acceptor.Handler(func(w http.ResponseWriter, _ *http.Request, who Principal) {
		seen = &who
		w.WriteHeader(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/kerberos", nil)
	if header != "" {
		request.Header.Set("Authorization", header)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder.Code, recorder.Header(), seen
}

func TestATicketForThisServiceSaysWhoseItIs(t *testing.T) {
	acceptor, err := Load(writtenKeytab(t, "the-service-secret"), "")
	if err != nil {
		t.Fatal(err)
	}

	status, _, who := served(t, acceptor, negotiate(t, "the-service-secret", "alice", testRealm))

	if status != http.StatusOK || who == nil {
		t.Fatalf("a valid ticket was answered %d without naming anybody", status)
	}

	if who.Name != "alice" || who.Realm != testRealm {
		t.Errorf("the ticket was read as %+v, want alice of %s", *who, testRealm)
	}
}

// A request without a ticket is asked for one, which is what makes a browser
// that trusts this address send it - and all a browser that does not ever sees.
func TestARequestWithoutATicketIsAskedForOne(t *testing.T) {
	acceptor, err := Load(writtenKeytab(t, "the-service-secret"), "")
	if err != nil {
		t.Fatal(err)
	}

	status, header, who := served(t, acceptor, "")

	if status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != "Negotiate" {
		t.Errorf("answered %d with %q, want 401 asking to negotiate", status, header.Get("WWW-Authenticate"))
	}

	if who != nil {
		t.Errorf("a request without a ticket was taken for %+v", *who)
	}
}

// A ticket sealed with somebody else's key is refused: it is what a forged one,
// or one meant for another service, looks like.
func TestATicketNotSealedForThisServiceIsRefused(t *testing.T) {
	acceptor, err := Load(writtenKeytab(t, "the-service-secret"), "")
	if err != nil {
		t.Fatal(err)
	}

	status, _, who := served(t, acceptor, negotiate(t, "another-secret", "alice", testRealm))

	if status == http.StatusOK || who != nil {
		t.Errorf("a ticket this service cannot open was answered %d and taken for %v", status, who)
	}
}

// A ticket from another realm is refused, however well sealed: alice of
// OTHER.EXAMPLE is not the alice the directory holds.
func TestATicketFromAnotherRealmIsRefused(t *testing.T) {
	acceptor, err := Load(writtenKeytab(t, "the-service-secret"), "")
	if err != nil {
		t.Fatal(err)
	}

	status, _, who := served(t, acceptor, negotiate(t, "the-service-secret", "alice", "OTHER.EXAMPLE"))

	if status == http.StatusOK || who != nil {
		t.Errorf("a ticket from another realm was answered %d and taken for %v", status, who)
	}
}

func TestAKeytabThatDoesNotHoldTheNamedServiceIsRefused(t *testing.T) {
	if _, err := Load(writtenKeytab(t, "the-service-secret"), "HTTP/elsewhere.example.com"); err == nil {
		t.Error("a keytab without the named spn was accepted")
	}

	acceptor, err := Load(writtenKeytab(t, "the-service-secret"), spn+"@"+testRealm)
	if err != nil {
		t.Fatalf("the keytab's own spn, named with its testRealm, was refused: %v", err)
	}

	if acceptor.Realm() != testRealm {
		t.Errorf("the testRealm read from the keytab is %q, want %q", acceptor.Realm(), testRealm)
	}
}
