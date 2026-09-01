// Package security holds the password primitives. They are isolated here so
// the hashing choice is made in exactly one place.
package security

import (
	"crypto/rand"
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is deliberately modest: this is an internal tool, and a
// length rule that forces workarounds helps nobody.
const MinPasswordLength = 8

// MaxPasswordLength is bcrypt's limit rather than a policy, and it is in bytes.
//
// bcrypt takes at most 72 bytes and refuses anything longer. There was no
// maximum here, so the refusal came out of the library - "bcrypt: password
// length exceeds 72 bytes" - and reached the person setting the password: an
// algorithm they did not choose, counted in a unit they did not type, and, since
// only ErrPasswordTooShort was mapped to a code, the one message in this API that
// arrived untranslated whatever language they were reading.
//
// Bytes, not characters, and that is the part worth stating out loud. "Correct
// Horse Battery Staple And Several More Words To Be Safe Against Guessing" is 79
// bytes and was refused. In German it bites sooner: an umlaut is two bytes in
// UTF-8, so a 60-character German passphrase can be over a limit that a
// 70-character English one is under.
//
// Not worked around by pre-hashing. Feeding bcrypt a digest would lift the limit
// and would also change what every stored hash means, which is a migration over
// everybody's password to buy a longer passphrase than anyone here is typing.
const MaxPasswordLength = 72

// ErrPasswordTooShort is returned by HashPassword for unusable input.
var ErrPasswordTooShort = errors.New("password must be at least 8 characters")

// ErrPasswordTooLong is returned for one past what bcrypt will take.
var ErrPasswordTooLong = errors.New("password must be at most 72 bytes")

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}

	// Before bcrypt is asked, so the reason is this application's own.
	if len(password) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// VerifyPassword reports whether the password matches the stored hash.
//
// An empty hash is compared too, against one nothing will match. There is no
// password it can accept, so the answer is the same false it always was - what
// changes is how long it takes to say it.
//
// bcrypt is where the time in a sign-in goes: a comparison costs tens of
// milliseconds and everything around it costs one. So a caller that skipped the
// comparison, because the address it looked up does not exist or belongs to
// somebody who signs in through the directory, answered far faster than one that
// did - and that difference is readable from outside with a stopwatch. Same
// message, same status, different duration, and the durations spell out which
// addresses are real.
func VerifyPassword(hash, password string) bool {
	stored := []byte(hash)
	if len(stored) == 0 {
		stored = nothingMatchesThis()
	}

	return bcrypt.CompareHashAndPassword(stored, []byte(password)) == nil
}

// nothingMatchesThis is a hash of a value nobody holds, at whatever cost a real
// password is stored with.
//
// Computed rather than written down, because a literal would be frozen at the
// cost that was current when it was pasted, and costing the same as a real
// comparison is the entire purpose of it. Computed once and on first use rather
// than at start-up, so a process that never verifies a password never pays for
// it.
var nothingMatchesThis = sync.OnceValue(func() []byte {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// A system with no randomness cannot be helped here, and a fixed value
		// still hashes at the right cost - which is what this is for.
		secret = []byte("no password is ever offered as this")
	}

	hash, err := bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
	if err != nil {
		// Only an invalid cost reaches this, and the cost is a constant. An
		// unusable hash still compares as a mismatch, one step earlier.
		return nil
	}

	return hash
})
