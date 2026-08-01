package model

import "time"

// APIToken lets a user call the API from a script without their password.
//
// A token carries no rights of its own: every request is evaluated against
// the owner's current role, so revoking a role revokes the token's reach at
// the same moment.
type APIToken struct {
	ID     uint
	UserID uint
	Name   string

	// TokenHash is all that is stored. The token itself is shown once, at
	// creation, and cannot be recovered afterwards.
	TokenHash string

	// Prefix is the readable head of the token, so a user can tell their
	// tokens apart in a list without the secret being present.
	Prefix string

	CreatedAt time.Time

	// ExpiresAt is optional; nil means the token does not expire on its own.
	ExpiresAt *time.Time

	// LastUsedAt makes an unused or a leaked-and-used token visible.
	LastUsedAt *time.Time
}

// Expired reports whether the token has passed its expiry.
func (t *APIToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)
}
