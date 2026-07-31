package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// sessionTokenBytes is the entropy behind a session token. 32 bytes is far
// beyond guessing range and keeps the cookie short enough to be unremarkable.
const sessionTokenBytes = 32

// NewSessionToken returns a fresh session token for the client.
//
// The caller stores only HashToken(token); the plain value exists solely in
// the cookie, so a leaked database cannot be turned into live sessions.
func NewSessionToken() (string, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken returns the storable form of a session token.
//
// A plain SHA-256 is right here, unlike for passwords: the input is 256 bits
// of randomness, so there is nothing to brute-force and a slow hash would only
// add latency to every request.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
