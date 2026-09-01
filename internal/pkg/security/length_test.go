package security_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
)

// A passphrase that is too long is refused in this application's words.
//
// bcrypt takes at most 72 bytes and refuses anything longer. This package had a
// minimum and no maximum, so the refusal came out of the library:
//
//	bcrypt: password length exceeds 72 bytes
//
// which reaches the person setting the password. It names an algorithm they did
// not choose, it counts in bytes rather than in anything they typed, and - because
// only ErrPasswordTooShort is mapped to a code - it is the one message in this
// API that arrives untranslated, in English, whatever language they are reading.
//
// The bytes matter and not only the words. "Correct Horse Battery Staple And
// Several More Words To Be Safe Against Guessing" is 79 bytes, which is a
// perfectly sensible passphrase and was refused. In German it is worse: an umlaut
// is two bytes in UTF-8, so a 60-character German passphrase can be over the
// limit while a 70-character English one is not.
func TestAPassphraseOverTheLimitIsRefusedAsTooLong(t *testing.T) {
	tooLong := "Correct Horse Battery Staple And Several More Words To Be Safe Against Guessing"

	if len(tooLong) <= security.MaxPasswordLength {
		t.Fatalf("the example is %d bytes, which is not over the %d-byte limit; "+
			"this case has to exercise the refusal to mean anything",
			len(tooLong), security.MaxPasswordLength)
	}

	_, err := security.HashPassword(tooLong)
	if err == nil {
		t.Fatal("a passphrase over the limit was hashed")
	}

	if !errors.Is(err, security.ErrPasswordTooLong) {
		t.Errorf("refused with %v, which is not this application's own reason", err)
	}

	if strings.Contains(err.Error(), "bcrypt") {
		t.Errorf("the refusal names the algorithm: %v", err)
	}
}

// The boundary is where it is said to be, and it is a boundary in bytes.
func TestTheLengthLimitCountsBytesAndIsInclusive(t *testing.T) {
	atTheLimit := strings.Repeat("a", security.MaxPasswordLength)

	if _, err := security.HashPassword(atTheLimit); err != nil {
		t.Errorf("a password of exactly %d bytes was refused: %v",
			security.MaxPasswordLength, err)
	}

	if _, err := security.HashPassword(atTheLimit + "a"); !errors.Is(err, security.ErrPasswordTooLong) {
		t.Errorf("one byte over the limit gave %v", err)
	}

	// Half as many characters, because each umlaut is two bytes - which is the
	// half of this that will surprise somebody.
	german := strings.Repeat("ä", security.MaxPasswordLength/2)

	if _, err := security.HashPassword(german); err != nil {
		t.Errorf("%d umlauts (%d bytes) were refused: %v",
			security.MaxPasswordLength/2, len(german), err)
	}

	if _, err := security.HashPassword(german + "ä"); !errors.Is(err, security.ErrPasswordTooLong) {
		t.Errorf("one umlaut over the limit gave %v", err)
	}
}
