package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

const apiTokenColumns = "id, user_id, name, token_hash, prefix, created_at, expires_at, last_used_at"

// APITokenRepository stores personal API tokens in a SQL database.
type APITokenRepository struct {
	base
}

// NewAPITokenRepository creates a token repository for the given dialect.
func NewAPITokenRepository(db DB, dialect string) *APITokenRepository {
	return &APITokenRepository{base{db: db, dialect: dialect}}
}

var _ repository.APITokenRepository = (*APITokenRepository)(nil)

func (r *APITokenRepository) Save(ctx context.Context, token *model.APIToken) (*model.APIToken, error) {
	id, err := r.insert(ctx,
		"INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at, expires_at) "+
			"VALUES (?, ?, ?, ?, ?, ?)",
		token.UserID, token.Name, token.TokenHash, token.Prefix, token.CreatedAt, token.ExpiresAt)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	created := *token
	created.ID = id

	return &created, nil
}

func (r *APITokenRepository) GetByHash(ctx context.Context, tokenHash string) (*model.APIToken, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+apiTokenColumns+" FROM api_tokens WHERE token_hash = ?"), tokenHash)

	token, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("token", "")
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return token, nil
}

func (r *APITokenRepository) ListForUser(ctx context.Context, userID uint) ([]*model.APIToken, error) {
	rows, err := r.db.QueryContext(ctx,
		r.rebind("SELECT "+apiTokenColumns+" FROM api_tokens WHERE user_id = ? ORDER BY id DESC"), userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	tokens := make([]*model.APIToken, 0)

	for rows.Next() {
		token, scanErr := scanAPIToken(rows)
		if scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		tokens = append(tokens, token)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return tokens, nil
}

// Delete removes a token, scoped to its owner so one user cannot revoke
// another's by guessing an id.
func (r *APITokenRepository) Delete(ctx context.Context, id, userID uint) error {
	affected, err := r.exec(ctx, "DELETE FROM api_tokens WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("token", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

func (r *APITokenRepository) TouchLastUsed(ctx context.Context, id uint) error {
	if _, err := r.exec(ctx, "UPDATE api_tokens SET last_used_at = ? WHERE id = ?", time.Now(), id); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func scanAPIToken(s scanner) (*model.APIToken, error) {
	var (
		token      model.APIToken
		createdAt  dateTime
		expiresAt  dateTime
		lastUsedAt dateTime
	)

	err := s.Scan(&token.ID, &token.UserID, &token.Name, &token.TokenHash, &token.Prefix,
		&createdAt, &expiresAt, &lastUsedAt)
	if err != nil {
		return nil, err
	}

	token.CreatedAt = createdAt.Time
	token.ExpiresAt = ptr(expiresAt)
	token.LastUsedAt = ptr(lastUsedAt)

	return &token, nil
}
