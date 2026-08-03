package model

import "time"

// Passkey is one credential a user registered for signing in.
//
// A passkey never leaves the device it was created on: what is stored here is
// the public half plus the counters needed to verify a signature. So unlike a
// password hash, nothing in this record is worth stealing - it cannot be
// replayed anywhere, and it identifies nobody outside this installation.
type Passkey struct {
	ID     uint
	UserID uint

	// Name is what the owner called it, so they can tell "work laptop" from
	// "phone" when revoking one.
	Name string

	// CredentialID is what the browser sends back to say which credential it
	// is signing with. Unique across the installation.
	CredentialID []byte

	// PublicKey verifies the signatures this credential produces.
	PublicKey []byte

	// AttestationType and Transports come from registration and are handed
	// back to the browser so it can prompt for the right kind of device.
	AttestationType string
	Transports      string

	// SignCount is the credential's own counter. An authenticator that
	// increments it lets a cloned credential be spotted: a signature arriving
	// with a count at or below the last one means two copies are in use.
	// Not every authenticator implements it, which is why zero is accepted.
	SignCount uint32

	// BackupEligible and BackedUp report whether the credential syncs to a
	// keychain. Worth recording because it changes what losing one device
	// means - a synced passkey survives it, a device-bound one does not.
	BackupEligible bool
	BackedUp       bool

	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// PasskeyMaxNameLength keeps a label readable in the list it appears in.
const PasskeyMaxNameLength = 120

// DefaultPasskeyName is used when someone registers without naming it.
const DefaultPasskeyName = "Passkey"
