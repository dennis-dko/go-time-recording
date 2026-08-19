package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// A Sealer encrypts the values this application has to be able to read back and
// whoever gets hold of the database must not.
//
// Passwords are not among them. A password is verified by hashing what was typed
// and comparing, so bcrypt is the right answer and nothing here improves on it.
// These are the values the application itself has to hand back out: a TOTP secret
// is fed to the code generator on every sign-in, and the directory's bind
// password is sent to the directory. Hashing them would make them useless.
//
// So they are encrypted, and the key lives outside the database. A stolen dump
// then yields neither passwords nor second factors nor the directory account -
// and a dump is the realistic loss here: a backup on a laptop, a snapshot copied
// to somewhere with weaker access, a managed database read by somebody who should
// only have been able to reach the application.
//
// It is not protection against somebody who has the machine. They have the key
// too. Nothing at this layer can change that, and pretending otherwise would be
// the more dangerous claim.
type Sealer struct {
	// aead is nil when no key is configured, which is how an installation that
	// has not set one keeps working. Seal then stores what it was given.
	aead cipher.AEAD
}

// sealedPrefix marks a value this package wrote.
//
// Present so a stored value says which it is. An installation that turns
// encryption on has rows written before it did, and a reader that had to guess
// would either refuse them or, worse, hand back ciphertext as though it were a
// secret.
const sealedPrefix = "gtr.v1:"

// KeyBytes is the key length, in bytes: AES-256.
const KeyBytes = 32

// ErrNoKey is returned when a sealed value is read by an installation that has
// no key configured - which means the key was removed, not that it was never set.
var ErrNoKey = errors.New("this value is encrypted and no key is configured")

// NewSealer builds a sealer from a base64 key. An empty key gives a sealer that
// stores values as they are, which is what an installation that has not
// configured one gets.
func NewSealer(key string) (*Sealer, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return &Sealer{}, nil
	}

	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("the key is not valid base64: %w", err)
	}

	if len(raw) != KeyBytes {
		return nil, fmt.Errorf("the key is %d bytes, and %d are needed: generate one with "+
			"`openssl rand -base64 %d`", len(raw), KeyBytes, KeyBytes)
	}

	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Sealer{aead: aead}, nil
}

// Enabled reports whether a key is configured.
func (s *Sealer) Enabled() bool { return s != nil && s.aead != nil }

// Seal encrypts a value for storage. Without a key it hands back what it was
// given, so the column holds what it always held.
//
// An empty value stays empty rather than becoming ciphertext: "no second factor"
// and "a second factor nobody can read" have to stay distinguishable, and every
// caller already treats the empty string as the first of those.
func (s *Sealer) Seal(plain string) (string, error) {
	if !s.Enabled() || plain == "" {
		return plain, nil
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("no randomness for a nonce: %w", err)
	}

	sealed := s.aead.Seal(nonce, nonce, []byte(plain), nil)

	return sealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reads a stored value back.
//
// A value without the marker is returned as it is. That is what an installation
// looks like before it configured a key, and what every row written before it did
// still looks like afterwards - so this is the upgrade path rather than a
// weakness: the marker is written by Seal and cannot be forged into a value the
// application would then trust, because a value without it was never trusted for
// anything except being itself.
func (s *Sealer) Open(stored string) (string, error) {
	if !strings.HasPrefix(stored, sealedPrefix) {
		return stored, nil
	}

	if !s.Enabled() {
		return "", ErrNoKey
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPrefix))
	if err != nil {
		return "", fmt.Errorf("the stored value is not valid base64: %w", err)
	}

	if len(raw) < s.aead.NonceSize() {
		return "", errors.New("the stored value is too short to be encrypted")
	}

	nonce, body := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]

	plain, err := s.aead.Open(nil, nonce, body, nil)
	if err != nil {
		// Deliberately not "the key is wrong": a value written by a different key
		// and a value somebody edited look identical from here, and both mean the
		// same thing to whoever has to fix it.
		return "", errors.New("this value cannot be decrypted with the configured key")
	}

	return string(plain), nil
}

// IsSealed reports whether a stored value was written by Seal. For the one-off
// pass that encrypts what an installation already had.
func IsSealed(stored string) bool { return strings.HasPrefix(stored, sealedPrefix) }
