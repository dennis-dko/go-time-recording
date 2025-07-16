package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// UserRepository repository functions for user
type UserRepository interface {
	Save(ctx context.Context, user *model.User) (*model.User, error)

	GetByID(ctx context.Context, id uint) (*model.User, error)

	GetAll(ctx context.Context) ([]*model.User, error)

	Update(ctx context.Context, user *model.User) (*model.User, error)

	Delete(ctx context.Context, id uint) error
}
