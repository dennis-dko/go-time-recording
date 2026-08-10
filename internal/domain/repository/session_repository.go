package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// SessionRepository stores browser sessions.
type SessionRepository interface {
	Save(ctx context.Context, session *model.Session) error

	// Get resolves a session by the stored hash of its token.
	Get(ctx context.Context, tokenHash string) (*model.Session, error)

	Delete(ctx context.Context, tokenHash string) error

	// DeleteForUser ends every session of one user, used when their password
	// or their rights change.
	DeleteForUser(ctx context.Context, userID uint) error

	// DeleteForUserExcept ends every session of one user but the one named, used
	// when they change their own password.
	//
	// Ending all of them is right when somebody else forced the change, and wrong
	// when they made it themselves: it signs them out of the screen they are
	// looking at, in the middle of a setup wizard whose next step needs the
	// session. The device in their hand is the one that just proved it knows the
	// old password, so it is the one to keep.
	DeleteForUserExcept(ctx context.Context, userID uint, keepTokenHash string) error

	// DeleteExpired prunes sessions that have timed out.
	DeleteExpired(ctx context.Context) (int64, error)
}
