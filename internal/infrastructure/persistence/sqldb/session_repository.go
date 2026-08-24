package sqldb

import (
	"context"
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
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at) "+
			"VALUES (?, ?, ?, ?, ?)",
		session.TokenHash, session.UserID, session.CreatedAt, session.ExpiresAt,
		session.LastSeenAt)
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *SessionRepository) Get(ctx context.Context, tokenHash string) (*model.Session, error) {
	row := r.db.QueryRowContext(ctx, r.rebind(
		"SELECT token_hash, user_id, created_at, expires_at, last_seen_at "+
			"FROM sessions WHERE token_hash = ?"), tokenHash)

	var (
		session    model.Session
		createdAt  dateTime
		expiresAt  dateTime
		lastSeenAt dateTime
	)

	err := row.Scan(&session.TokenHash, &session.UserID, &createdAt, &expiresAt, &lastSeenAt)
	if problem := problemReading(err, "session", ""); problem != nil {
		return nil, problem
	}

	session.CreatedAt = createdAt.Time
	session.ExpiresAt = expiresAt.Time
	session.LastSeenAt = lastSeenAt.Time

	// A row written before the column existed, or by a version that did not
	// fill it. Its sign-in is the last thing known about it, which is the
	// honest answer and the same one the migration gave the rows it found.
	if session.LastSeenAt.IsZero() {
		session.LastSeenAt = session.CreatedAt
	}

	return &session, nil
}

// Touch records that the session was used, for the idle timeout to measure
// against.
//
// One statement, no read-back: the caller has the row already and only the
// moment matters. It is deliberately not part of Save - a session is opened once
// and used thousands of times, and the two want different SQL.
func (r *SessionRepository) Touch(ctx context.Context, tokenHash string, at time.Time) error {
	if _, err := r.exec(ctx,
		"UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?", at, tokenHash); err != nil {
		return apperror.Internal(err)
	}

	return nil
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

// DeleteForUserExcept ends every session of a user but the one named.
func (r *SessionRepository) DeleteForUserExcept(
	ctx context.Context,
	userID uint,
	keepTokenHash string,
) error {
	if _, err := r.exec(ctx,
		"DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?",
		userID, keepTokenHash); err != nil {
		return apperror.Internal(err)
	}

	return nil
}
