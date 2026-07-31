// Package security holds the password primitives. They are isolated here so
// the hashing choice is made in exactly one place.
package security

import (
	"errors"

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
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
