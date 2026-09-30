package kerberos

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jcmturner/goidentity/v6"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/service"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

// Principal is whose ticket a browser presented: jdoe of EXAMPLE.COM.
type Principal struct {
	Name  string
	Realm string
}

// Acceptor checks the Kerberos tickets browsers present with SPNEGO.
type Acceptor struct {
	keytab   *keytab.Keytab
	realm    string
	settings []func(*service.Settings)
	logger   Logger
}

// Logger is where a refused ticket says why: the part of GoFr's logger this
// needs.
type Logger interface {
	Warnf(format string, args ...any)
	Debugf(format string, args ...any)
}

// Load reads the keytab at path.
//
// servicePrincipal names the key to use - "HTTP/zeit.example.com", with or
// without its realm - and is needed only when the keytab holds more than one
// service. Its realm is the only one whose tickets are taken: a directory is
// asked about jdoe, and jdoe of a realm this installation's directory does not
// hold is somebody else.
//
// logger hears why a ticket was refused, and may be nil. A browser whose ticket
// is refused falls back to the sign-in form as quietly as one that has none, so
// without it a clock too far from the KDC's, a key the keytab does not hold or a
// browser offering NTLM instead leaves no trace anywhere.
func Load(path, servicePrincipal string, logger Logger) (*Acceptor, error) {
	kt, err := keytab.Load(path)
	if err != nil {
		return nil, fmt.Errorf("reading the keytab %s: %w", path, err)
	}

	realm, err := realmOf(kt, servicePrincipal)
	if err != nil {
		return nil, err
	}

	// The PAC - the group memberships Active Directory writes into a ticket - is
	// not read: nothing here decides anything by it, and a directory lookup
	// answers who this is. Decoding it is one more way for a valid ticket to be
	// refused.
	settings := []func(*service.Settings){service.DecodePAC(false)}

	if name, _, _ := strings.Cut(servicePrincipal, "@"); name != "" {
		settings = append(settings, service.KeytabPrincipal(name))
	}

	if logger != nil {
		settings = append(settings, service.Logger(log.New(gokrb5Lines{logger}, "", 0)))
	}

	return &Acceptor{keytab: kt, realm: realm, settings: settings, logger: logger}, nil
}

// gokrb5Lines passes gokrb5's log on: a refusal as a warning, since it names the
// reason, and an acceptance at debug, since it names the person - a line for
// every sign-in is somebody's name in the log for every sign-in.
//
// Told apart by gokrb5's wording, which is all there is to tell them by. Were it
// to change, acceptances would arrive as warnings: noisy, and never a refusal
// gone missing.
type gokrb5Lines struct{ logger Logger }

func (g gokrb5Lines) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))

	if strings.HasSuffix(line, "SPNEGO authentication succeeded") {
		g.logger.Debugf("Kerberos: %s", line)
	} else {
		g.logger.Warnf("Kerberos: %s", line)
	}

	return len(p), nil
}

// realmOf is the realm of the key servicePrincipal names, or of the only service
// the keytab holds.
func realmOf(kt *keytab.Keytab, servicePrincipal string) (string, error) {
	if len(kt.Entries) == 0 {
		return "", errors.New("the keytab holds no keys")
	}

	name, realm, _ := strings.Cut(servicePrincipal, "@")

	for _, entry := range kt.Entries {
		held := strings.Join(entry.Principal.Components, "/")

		if name != "" && !strings.EqualFold(held, name) {
			continue
		}

		if realm != "" && !strings.EqualFold(entry.Principal.Realm, realm) {
			continue
		}

		return entry.Principal.Realm, nil
	}

	return "", fmt.Errorf("the keytab holds no key for %s", servicePrincipal)
}

// Realm is the one realm whose tickets are taken.
func (a *Acceptor) Realm() string { return a.realm }

// Handler hands next the principal of a request that carries a valid ticket for
// this service.
//
// A request carrying none is answered 401 with the Negotiate challenge, which is
// what makes a browser that trusts this address send one - and a browser that
// does not, or has no ticket, simply keeps the 401, which is the whole of the
// quiet fallback to the sign-in form. A ticket for another service, a forged one
// and one from another realm are refused the same way.
func (a *Acceptor) Handler(next func(w http.ResponseWriter, r *http.Request, who Principal)) http.Handler {
	accepted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := goidentity.FromHTTPRequestContext(r)
		if id == nil || !id.Authenticated() || !strings.EqualFold(id.Domain(), a.realm) {
			// gokrb5 accepted this one, so the refusal is ours to explain.
			if id != nil && a.logger != nil {
				a.logger.Warnf("Kerberos: a ticket of realm %s was refused; only %s's are taken",
					id.Domain(), a.realm)
			}

			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

			return
		}

		next(w, r, Principal{Name: id.UserName(), Realm: id.Domain()})
	})

	return spnego.SPNEGOKRB5Authenticate(accepted, a.keytab, a.settings...)
}
