package tlsserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The plain port answers the machine it runs on, and redirects the network.
//
// GoFr binds every interface and offers no way to say otherwise, so terminating
// TLS in front of it leaves the unencrypted port open beside the encrypted one.
// That port is not only traffic in the clear: the rate limiter keys on
// X-Forwarded-For, so a caller inventing a fresh one per attempt had no limit on
// sign-in guesses, and X-Forwarded-Proto decided whether cookies were marked
// Secure. Both of those are headers, and headers are free to whoever can reach
// the port.
func TestThePlainPortAnswersOnlyThisMachine(t *testing.T) {
	serving := func() bool { return true }

	reached := false
	guarded := KeepThePlainPortLocal(serving, 443)(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { reached = true }))

	for _, peer := range []string{"127.0.0.1:51000", "[::1]:51000"} {
		t.Run("from "+peer, func(t *testing.T) {
			reached = false
			rec := httptest.NewRecorder()

			r := httptest.NewRequest(http.MethodGet, "http://gtr.example.com:8000/api/v1/me", nil)
			r.RemoteAddr = peer

			guarded.ServeHTTP(rec, r)

			if !reached {
				t.Errorf("the front end's own request was turned away with %d - "+
					"it dials loopback, so this is the whole of the traffic",
					rec.Code)
			}
		})
	}

	t.Run("from the network", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()

		r := httptest.NewRequest(http.MethodPost, "http://gtr.example.com:8000/api/v1/auth/login", nil)
		r.RemoteAddr = "203.0.113.7:41234"
		// The header that made this worth closing rather than documenting.
		r.Header.Set("X-Forwarded-For", "198.51.100.1")

		guarded.ServeHTTP(rec, r)

		if reached {
			t.Fatal("a request off the network was served over plain HTTP while " +
				"HTTPS was running beside it")
		}

		// 308 rather than 302: a redirected POST that quietly became a GET would
		// lose the body it was carrying.
		if rec.Code != http.StatusPermanentRedirect {
			t.Errorf("the network was answered with %d, want %d",
				rec.Code, http.StatusPermanentRedirect)
		}

		if to := rec.Header().Get("Location"); to != "https://gtr.example.com/api/v1/auth/login" {
			t.Errorf("redirected to %q, which is not the encrypted address of the "+
				"same host", to)
		}
	})

	// And the condition this rests on. An installation whose port 443 was
	// refused still serves on the plain one; sending it to the encrypted address
	// would turn a line in the log into an outage.
	t.Run("no front end, no redirect", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()

		down := KeepThePlainPortLocal(func() bool { return false }, 443)(http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) { reached = true }))

		r := httptest.NewRequest(http.MethodGet, "http://gtr.example.com:8000/", nil)
		r.RemoteAddr = "203.0.113.7:41234"

		down.ServeHTTP(rec, r)

		if !reached {
			t.Errorf("with no HTTPS listener up, the plain port answered %d "+
				"instead of serving - the installation is now unreachable", rec.Code)
		}
	})
}

// A refused HTTPS bind is a failed start, not a log line.
//
// ListenAndServeTLS binds and serves in one call, so starting it in a goroutine
// meant that port 443 without the privilege to take it - or already held by
// something else - was reported to a log nobody was reading, while Start handed
// back a shutdown function that its caller read as success. The installation
// then came up serving plain HTTP under a setting that says otherwise, and
// everything downstream that trusts "HTTPS is up" trusted a lie.
func TestAPortThatCannotBeBoundIsReported(t *testing.T) {
	// Held on every interface, which is where Start binds. Holding only
	// 127.0.0.1 is not a conflict on Windows, so the test would pass by not
	// testing anything.
	held, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("cannot take a port to hold: %v", err)
	}

	defer func() { _ = held.Close() }()

	taken := held.Addr().(*net.TCPAddr).Port

	certificate, key := selfSigned(t)

	stop, err := Start(Config{
		HTTPSPort: taken,
		HTTPPort:  0,
		Backend:   "127.0.0.1:1",
		CertFile:  certificate,
		KeyFile:   key,
	}, quietLogger{})

	if err == nil {
		if stop != nil {
			_ = stop(t.Context())
		}

		t.Fatal("a port that could not be bound was reported as a running HTTPS " +
			"server, which is how an installation serves plain HTTP believing " +
			"it does not")
	}

	if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("the refusal %q does not say what failed to bind", err)
	}
}

// selfSigned writes a certificate and key this test can hand to Start, so the
// bind is what fails rather than the certificate before it.
func selfSigned(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("cannot make a key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "gtr.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"gtr.example.com"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cannot make a certificate: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "fullchain.pem")
	keyFile = filepath.Join(dir, "privkey.pem")

	marshalled, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("cannot encode the key: %v", err)
	}

	write := func(path, kind string, bytes []byte) {
		if err := os.WriteFile(path,
			pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: bytes}), 0o600); err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
	}

	write(certFile, "CERTIFICATE", der)
	write(keyFile, "EC PRIVATE KEY", marshalled)

	return certFile, keyFile
}
