package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// APITokenRepository stores the personal API tokens.
type APITokenRepository interface {
	Save(ctx context.Context, token *model.APIToken) (*model.APIToken, error)

	// GetByHash resolves a presented token. It is the hot path, called on
	// every token-authenticated request.
	GetByHash(ctx context.Context, tokenHash string) (*model.APIToken, error)

	ListForUser(ctx context.Context, userID uint) ([]*model.APIToken, error)

	Delete(ctx context.Context, id, userID uint) error

	// TouchLastUsed records that the token was just used.
	TouchLastUsed(ctx context.Context, id uint) error
}
