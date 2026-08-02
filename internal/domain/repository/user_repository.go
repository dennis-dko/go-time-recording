package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// UserRepository repository functions for user
type UserRepository interface {
	Save(ctx context.Context, user *model.User) (*model.User, error)

	GetByID(ctx context.Context, id uint) (*model.User, error)

	// GetByEmail resolves the login identifier used for authentication.
	GetByEmail(ctx context.Context, email string) (*model.User, error)

	// GetByExternalID resolves a directory-backed account by the identifier
	// the directory assigned it. That identifier outlives a rename, which the
	// mail address does not.
	GetByExternalID(ctx context.Context, externalID string) (*model.User, error)

	GetAll(ctx context.Context) ([]*model.User, error)

	Update(ctx context.Context, user *model.User) (*model.User, error)

	Delete(ctx context.Context, id uint) error
}
