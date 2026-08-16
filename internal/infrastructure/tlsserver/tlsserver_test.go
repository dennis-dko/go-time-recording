package tlsserver

import (
	"strings"
	"testing"
)

// TLS needs either a name to be given a certificate for, or one already held.
//
// Without either there is nothing to serve HTTPS with, and the failure worth
// preventing is the quiet one: starting anyway, serving plain HTTP, under a
// setting that says the opposite.
func TestTLSNeedsEitherANameOrACertificate(t *testing.T) {
	_, err := Start(Config{}, quietLogger{})

	if err == nil {
		t.Fatal("TLS started with neither a domain nor a certificate")
	}

	// The message names both ways out, because an installation reaching this has
	// picked neither and the answer depends on which it can have.
	for _, want := range []string{"TLS_DOMAINS", "TLS_CERT_FILE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %s", err, want)
		}
	}
}

// A certificate that cannot be read stops the start.
//
// Read once, here, rather than per request: a certificate whose mistakes are
// found lazily is one whose mistakes are found by the first visitor, on a
// connection that then fails for reasons nobody can see from outside.
func TestACertificateThatCannotBeReadStopsTheStart(t *testing.T) {
	_, err := Start(Config{
		CertFile: "nowhere/fullchain.pem",
		KeyFile:  "nowhere/privkey.pem",
	}, quietLogger{})

	if err == nil {
		t.Fatal("a missing certificate started anyway")
	}

	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("the refusal %q does not say what could not be read", err)
	}
}

// Having a certificate is what decides the route, and both files are needed for
// it: one on its own is a half-configured installation, and falling back to
// Let's Encrypt for it would ask a public authority about a private name.
func TestBothFilesAreNeededToCountAsHavingOne(t *testing.T) {
	for name, cfg := range map[string]Config{
		"neither":          {},
		"only the chain":   {CertFile: "a.pem"},
		"only the key":     {KeyFile: "b.pem"},
		"blank, not empty": {CertFile: "  ", KeyFile: "\t"},
	} {
		if cfg.usesOwnCertificate() {
			t.Errorf("%s counts as having a certificate", name)
		}
	}

	if !(Config{CertFile: "a.pem", KeyFile: "b.pem"}).usesOwnCertificate() {
		t.Error("a certificate and a key do not count as having one")
	}
}

type quietLogger struct{}

func (quietLogger) Infof(string, ...any)  {}
func (quietLogger) Errorf(string, ...any) {}
