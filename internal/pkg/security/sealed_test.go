package security_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

func aKey(t *testing.T) string {
	t.Helper()

	raw := make([]byte, security.KeyBytes)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("no randomness: %v", err)
	}

	return base64.StdEncoding.EncodeToString(raw)
}

// What goes in comes back, and what is stored is not what went in.
func TestASealedValueRoundTripsAndDoesNotShowThrough(t *testing.T) {
	sealer, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build a sealer: %v", err)
	}

	const secret = "JBSWY3DPEHPK3PXP"

	stored, err := sealer.Seal(secret)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if strings.Contains(stored, secret) {
		t.Errorf("the stored value contains the secret it was meant to hide: %q", stored)
	}

	if !security.IsSealed(stored) {
		t.Errorf("the stored value carries no marker: %q", stored)
	}

	back, err := sealer.Open(stored)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if back != secret {
		t.Errorf("read back %q, want %q", back, secret)
	}
}

// The same value twice must not produce the same ciphertext.
//
// Two people enrolling the same authenticator, or one account's secret written
// twice, would otherwise be visible as equal in the column - which says something
// about the plaintext without decrypting anything.
func TestTheSameSecretSealsDifferentlyEachTime(t *testing.T) {
	sealer, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build a sealer: %v", err)
	}

	first, err := sealer.Seal("the same secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	second, err := sealer.Seal("the same secret")
	if err != nil {
		t.Fatalf("seal again: %v", err)
	}

	if first == second {
		t.Error("sealing one value twice produced the same stored value, so equal " +
			"secrets are visible as equal in the column")
	}
}

// A different key cannot read it, and says so rather than handing back rubbish.
func TestAnotherKeyCannotOpenIt(t *testing.T) {
	mine, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	theirs, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	stored, err := mine.Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := theirs.Open(stored); err == nil {
		t.Fatal("a value sealed with one key was opened with another")
	}
}

// An installation that never configured a key keeps working, and its columns
// hold what they always held.
func TestWithoutAKeyValuesArePassedThrough(t *testing.T) {
	sealer, err := security.NewSealer("")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if sealer.Enabled() {
		t.Error("an empty key produced an enabled sealer")
	}

	stored, err := sealer.Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if stored != "JBSWY3DPEHPK3PXP" {
		t.Errorf("without a key the value was changed to %q", stored)
	}
}

// The upgrade path: rows written before a key was configured are still readable
// after one is.
func TestValuesWrittenBeforeTheKeyAreStillReadable(t *testing.T) {
	sealer, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	back, err := sealer.Open("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("a value from before the key was refused: %v", err)
	}

	if back != "JBSWY3DPEHPK3PXP" {
		t.Errorf("read back %q, want the value as it was stored", back)
	}

	if security.IsSealed("JBSWY3DPEHPK3PXP") {
		t.Error("a plain value is being reported as sealed, so the one-off pass " +
			"would skip it")
	}
}

// And the reverse, which is the mistake worth naming: a key that was configured
// and then removed. Every sealed value becomes unreadable, and saying so is the
// only useful answer.
func TestRemovingTheKeyIsReported(t *testing.T) {
	sealer, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	stored, err := sealer.Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	none, err := security.NewSealer("")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, err := none.Open(stored); !errors.Is(err, security.ErrNoKey) {
		t.Errorf("opening a sealed value with no key answered %v, want %v",
			err, security.ErrNoKey)
	}
}

// Empty stays empty: "no second factor" and "a second factor nobody can read"
// have to stay different things.
func TestNothingSealsToNothing(t *testing.T) {
	sealer, err := security.NewSealer(aKey(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	stored, err := sealer.Seal("")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if stored != "" {
		t.Errorf("an empty value was stored as %q, which reads as a secret", stored)
	}
}

// A key that cannot work is refused at the point it is read, not at the point
// somebody signs in.
func TestAnUnusableKeyIsRefused(t *testing.T) {
	for name, key := range map[string]string{
		"not base64": "this is not base64 !!",
		"too short":  base64.StdEncoding.EncodeToString([]byte("sixteen bytes...")),
		"too long":   base64.StdEncoding.EncodeToString(make([]byte, 64)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := security.NewSealer(key); err == nil {
				t.Error("an unusable key was accepted")
			}
		})
	}
}
