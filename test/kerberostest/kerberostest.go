package kerberostest

import (
	"encoding/base64"
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

// Service is the one service the tests' tickets are for, and Realm its realm.
const (
	Service = "HTTP/zeit.example.com"
	Realm   = "EXAMPLE.COM"
)

// Keytab holds the service's key, derived from secret.
func Keytab(t *testing.T, secret string) *keytab.Keytab {
	t.Helper()

	kt := keytab.New()
	if err := kt.AddEntry(Service, Realm, secret, time.Now(), 1, etypeID.AES256_CTS_HMAC_SHA1_96); err != nil {
		t.Fatalf("building the keytab: %v", err)
	}

	return kt
}

// KeytabFile is Keytab on disk, where an operator would place it, and its path.
func KeytabFile(t *testing.T, dir, secret string) string {
	t.Helper()

	raw, err := Keytab(t, secret).Marshal()
	if err != nil {
		t.Fatalf("writing the keytab: %v", err)
	}

	path := filepath.Join(dir, "service.keytab")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("writing the keytab: %v", err)
	}

	return path
}

// Negotiate is the Authorization header a browser sends: a ticket for the
// service, issued to name of clientRealm and sealed with the key secret derives.
func Negotiate(t *testing.T, secret, name, clientRealm string) string {
	t.Helper()

	now := time.Now().UTC()

	ticket, sessionKey, err := messages.NewTicket(
		types.NewPrincipalName(nametype.KRB_NT_PRINCIPAL, name), clientRealm,
		types.NewPrincipalName(nametype.KRB_NT_SRV_INST, Service), Realm,
		types.NewKrbFlags(), Keytab(t, secret), etypeID.AES256_CTS_HMAC_SHA1_96, 1,
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
