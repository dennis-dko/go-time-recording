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

// ErrPasswordTooShort is returned by HashPassword for unusable input.
var ErrPasswordTooShort = errors.New("password must be at least 8 characters")

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", ErrPasswordTooShort
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
