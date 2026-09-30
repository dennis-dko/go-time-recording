package kerberos

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/kerberostest"
)

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

func acceptorFor(t *testing.T, secret string) *Acceptor {
	t.Helper()

	acceptor, err := Load(kerberostest.KeytabFile(t, t.TempDir(), secret), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	return acceptor
}

func TestATicketForThisServiceSaysWhoseItIs(t *testing.T) {
	status, _, who := served(t, acceptorFor(t, "the-service-secret"),
		kerberostest.Negotiate(t, "the-service-secret", "alice", kerberostest.Realm))

	if status != http.StatusOK || who == nil {
		t.Fatalf("a valid ticket was answered %d without naming anybody", status)
	}

	if who.Name != "alice" || who.Realm != kerberostest.Realm {
		t.Errorf("the ticket was read as %+v, want alice of %s", *who, kerberostest.Realm)
	}
}

// A request without a ticket is asked for one, which is what makes a browser
// that trusts this address send it - and all a browser that does not ever sees.
func TestARequestWithoutATicketIsAskedForOne(t *testing.T) {
	status, header, who := served(t, acceptorFor(t, "the-service-secret"), "")

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
	status, _, who := served(t, acceptorFor(t, "the-service-secret"),
		kerberostest.Negotiate(t, "another-secret", "alice", kerberostest.Realm))

	if status == http.StatusOK || who != nil {
		t.Errorf("a ticket this service cannot open was answered %d and taken for %v", status, who)
	}
}

// A ticket from another realm is refused, however well sealed: alice of
// OTHER.EXAMPLE is not the alice the directory holds.
func TestATicketFromAnotherRealmIsRefused(t *testing.T) {
	status, _, who := served(t, acceptorFor(t, "the-service-secret"),
		kerberostest.Negotiate(t, "the-service-secret", "alice", "OTHER.EXAMPLE"))

	if status == http.StatusOK || who != nil {
		t.Errorf("a ticket from another realm was answered %d and taken for %v", status, who)
	}
}

func TestAKeytabThatDoesNotHoldTheNamedServiceIsRefused(t *testing.T) {
	path := kerberostest.KeytabFile(t, t.TempDir(), "the-service-secret")

	if _, err := Load(path, "HTTP/elsewhere.example.com", nil); err == nil {
		t.Error("a keytab without the named service was accepted")
	}

	acceptor, err := Load(path, kerberostest.Service+"@"+kerberostest.Realm, nil)
	if err != nil {
		t.Fatalf("the keytab's own service, named with its realm, was refused: %v", err)
	}

	if acceptor.Realm() != kerberostest.Realm {
		t.Errorf("the realm read from the keytab is %q, want %q", acceptor.Realm(), kerberostest.Realm)
	}
}

// logged keeps what the acceptor said, by level.
type logged struct{ warnings, debug []string }

func (l *logged) Warnf(format string, args ...any) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}

func (l *logged) Debugf(format string, args ...any) {
	l.debug = append(l.debug, fmt.Sprintf(format, args...))
}

// A refused ticket says why, where somebody working out why a browser fell back
// to the form will look; an accepted one names its owner at debug and no higher.
func TestARefusedTicketSaysWhyInTheLog(t *testing.T) {
	var log logged

	acceptor, err := Load(kerberostest.KeytabFile(t, t.TempDir(), "the-service-secret"), "", &log)
	if err != nil {
		t.Fatal(err)
	}

	served(t, acceptor, kerberostest.Negotiate(t, "another-secret", "alice", kerberostest.Realm))

	if len(log.warnings) != 1 {
		t.Fatalf("a ticket sealed with another key left %d warnings, want the one saying why: %q",
			len(log.warnings), log.warnings)
	}

	served(t, acceptor, kerberostest.Negotiate(t, "the-service-secret", "alice", "OTHER.EXAMPLE"))

	if len(log.warnings) != 2 || !strings.Contains(log.warnings[1], "OTHER.EXAMPLE") {
		t.Errorf("a ticket of another realm was refused without naming the realm: %q", log.warnings)
	}

	served(t, acceptor, kerberostest.Negotiate(t, "the-service-secret", "alice", kerberostest.Realm))

	if len(log.warnings) != 2 {
		t.Errorf("an accepted ticket was logged as a warning: %q", log.warnings)
	}

	if len(log.debug) == 0 || !strings.Contains(log.debug[len(log.debug)-1], "alice@"+kerberostest.Realm) {
		t.Errorf("an accepted ticket was not logged at debug with its owner: %q", log.debug)
	}
}
