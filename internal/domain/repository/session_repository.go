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

	// DeleteExpired prunes sessions that have timed out.
	DeleteExpired(ctx context.Context) (int64, error)
}
