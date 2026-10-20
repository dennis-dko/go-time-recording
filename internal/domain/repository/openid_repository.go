package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// OpenIDIdentityRepository keeps which account each provider identity is linked to.
type OpenIDIdentityRepository interface {
	// ForUser returns the identity an account is linked to, or a not-found error.
	ForUser(ctx context.Context, userID uint) (*model.OpenIDIdentity, error)

	// BySubject returns the link of a provider's subject, or a not-found error. It
	// is what every sign-in through a provider is looked up by.
	BySubject(ctx context.Context, issuer, subject string) (*model.OpenIDIdentity, error)

	// Link stores an account's identity in place of the one it had.
	Link(ctx context.Context, identity *model.OpenIDIdentity) error

	// Unlink removes an account's identity, and reports whether there was one.
	Unlink(ctx context.Context, userID uint) (bool, error)
}
