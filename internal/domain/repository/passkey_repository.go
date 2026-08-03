package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// PasskeyRepository stores the credentials users registered for signing in.
type PasskeyRepository interface {
	Save(ctx context.Context, passkey *model.Passkey) (*model.Passkey, error)

	// GetByUser lists one user's credentials, for the account screen and for
	// building the challenge they are asked to sign.
	GetByUser(ctx context.Context, userID uint) ([]*model.Passkey, error)

	// GetByCredentialID resolves the credential the browser signed with. This
	// is what makes a sign-in without a username possible: the credential
	// names its own owner.
	GetByCredentialID(ctx context.Context, credentialID []byte) (*model.Passkey, error)

	// TouchUsage records a successful sign-in and the counter the
	// authenticator reported, which is what makes a cloned credential
	// detectable.
	TouchUsage(ctx context.Context, id uint, signCount uint32) error

	// Delete removes one credential, scoped to its owner so nobody can revoke
	// somebody else's by guessing an id.
	Delete(ctx context.Context, id, userID uint) error
}
