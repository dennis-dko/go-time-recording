package service

import (
	"encoding/binary"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// webAuthnUser adapts a user to what the WebAuthn library expects.
//
// It exists so the domain model does not have to implement a library's
// interface: model.User describes a person in this application, not a WebAuthn
// participant, and letting the library's shape leak into it would tie the two
// together for no benefit.
type webAuthnUser struct {
	user *model.User

	// credentials is filled only where the ceremony needs them - signing in
	// with one - so registration does not read every credential to add one.
	credentials []*model.Passkey
}

var _ webauthn.User = (*webAuthnUser)(nil)

// WebAuthnID is the handle the authenticator stores alongside the credential.
//
// The numeric id rather than the mail address: it never changes, while an
// address does, and a credential bound to a stale address would be orphaned by
// an ordinary rename.
func (u *webAuthnUser) WebAuthnID() []byte { return webAuthnID(u.user.ID) }

// webAuthnID encodes a user id as the fixed-width handle WebAuthn expects.
func webAuthnID(id uint) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(id))

	return buf
}

// WebAuthnName is what the account is called on the device.
func (u *webAuthnUser) WebAuthnName() string { return u.user.Email }

// WebAuthnDisplayName is what the prompt shows.
func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u.user.Name != "" {
		return u.user.Name
	}

	return u.user.Email
}

// WebAuthnCredentials converts the stored records back into what the library
// verifies signatures against.
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.credentials))

	for _, passkey := range u.credentials {
		out = append(out, webauthn.Credential{
			ID:              passkey.CredentialID,
			PublicKey:       passkey.PublicKey,
			AttestationType: passkey.AttestationType,
			Flags: webauthn.CredentialFlags{
				UserPresent:    true,
				UserVerified:   true,
				BackupEligible: passkey.BackupEligible,
				BackupState:    passkey.BackedUp,
			},
			Authenticator: webauthn.Authenticator{
				SignCount: passkey.SignCount,
			},
			Transport: parseTransports(passkey.Transports),
		})
	}

	return out
}

// parseTransports turns the stored list back into the library's type.
func parseTransports(stored string) []protocol.AuthenticatorTransport {
	if stored == "" {
		return nil
	}

	var out []protocol.AuthenticatorTransport

	start := 0

	for i := 0; i <= len(stored); i++ {
		if i == len(stored) || stored[i] == ',' {
			if part := stored[start:i]; part != "" {
				out = append(out, protocol.AuthenticatorTransport(part))
			}

			start = i + 1
		}
	}

	return out
}
