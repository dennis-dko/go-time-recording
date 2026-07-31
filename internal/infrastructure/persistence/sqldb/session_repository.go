package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// SessionRepository stores sessions in the database rather than in memory, so
// signing in survives a restart of the binary.
type SessionRepository struct {
	base
}

// NewSessionRepository creates a session repository for the given dialect.
func NewSessionRepository(db DB, dialect string) *SessionRepository {
	return &SessionRepository{base{db: db, dialect: dialect}}
}

var _ repository.SessionRepository = (*SessionRepository)(nil)

func (r *SessionRepository) Save(ctx context.Context, session *model.Session) error {
	_, err := r.exec(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		session.TokenHash, session.UserID, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *SessionRepository) Get(ctx context.Context, tokenHash string) (*model.Session, error) {
	row := r.db.QueryRowContext(ctx, r.rebind(
		"SELECT token_hash, user_id, created_at, expires_at FROM sessions WHERE token_hash = ?"), tokenHash)

	var (
		session   model.Session
		createdAt dateTime
		expiresAt dateTime
	)

	err := row.Scan(&session.TokenHash, &session.UserID, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("session", "")
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	session.CreatedAt = createdAt.Time
	session.ExpiresAt = expiresAt.Time

	return &session, nil
}

func (r *SessionRepository) Delete(ctx context.Context, tokenHash string) error {
	if _, err := r.exec(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *SessionRepository) DeleteForUser(ctx context.Context, userID uint) error {
	if _, err := r.exec(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	removed, err := r.exec(ctx, "DELETE FROM sessions WHERE expires_at < ?", time.Now())
	if err != nil {
		return 0, apperror.Internal(err)
	}

	return removed, nil
}
