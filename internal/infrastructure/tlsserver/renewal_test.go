package tlsserver

import (
	"os"
	"testing"
	"time"
)

// A certificate renewed on disk is served without a restart.
//
// Read once at start, the renewed files were served from the next restart: the
// old certificate went on being served until it expired, and then every browser
// refused the address. The handshake looks at the files again at most once a
// certificateRecheck; the case lets the next one look at once.
func TestARenewedCertificateIsServedWithoutARestart(t *testing.T) {
	t.Parallel()

	certFile, keyFile := selfSigned(t, "old.example.com")

	files, err := loadCertificateFiles(certFile, keyFile, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}

	renewedCert, renewedKey := selfSigned(t, "renewed.example.com")

	// Written over the served ones, as a renewal writes them, and stamped later
	// than the first so a coarse clock on the filesystem cannot hide the change.
	later := time.Now().Add(2 * time.Second)

	for from, to := range map[string]string{renewedCert: certFile, renewedKey: keyFile} {
		renewed, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(to, renewed, 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.Chtimes(to, later, later); err != nil {
			t.Fatal(err)
		}
	}

	files.looked = time.Time{}

	served, err := files.certificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := namesIn(served.Leaf); got != "renewed.example.com" {
		t.Errorf("after the files were renewed the handshake was given the certificate for %q", got)
	}
}

// A certificate written before its key is not taken for a renewal: the pair in
// hand goes on being served until the two match.
func TestACertificateWithoutItsKeyKeepsThePairInHand(t *testing.T) {
	t.Parallel()

	certFile, keyFile := selfSigned(t, "old.example.com")

	files, err := loadCertificateFiles(certFile, keyFile, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}

	renewedCert, _ := selfSigned(t, "renewed.example.com")

	renewed, err := os.ReadFile(renewedCert)
	if err != nil {
		t.Fatal(err)
	}

	later := time.Now().Add(2 * time.Second)

	if err := os.WriteFile(certFile, renewed, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chtimes(certFile, later, later); err != nil {
		t.Fatal(err)
	}

	files.looked = time.Time{}

	served, err := files.certificate(nil)
	if err != nil || served == nil {
		t.Fatalf("the handshake was given nothing: %v", err)
	}

	if got := namesIn(served.Leaf); got != "old.example.com" {
		t.Errorf("a certificate without its key replaced the pair in hand: now %q", got)
	}
}
