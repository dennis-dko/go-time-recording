package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// RoleRepository repository functions for roles
type RoleRepository interface {
	Save(ctx context.Context, role *model.Role) (*model.Role, error)

	GetByID(ctx context.Context, id uint) (*model.Role, error)

	GetByName(ctx context.Context, name string) (*model.Role, error)

	GetAll(ctx context.Context) ([]*model.Role, error)

	Update(ctx context.Context, role *model.Role) (*model.Role, error)

	Delete(ctx context.Context, id uint) error

	// CountUsers reports how many users hold the role, so deleting a role in
	// use can be refused instead of orphaning its members.
	CountUsers(ctx context.Context, roleID uint) (int, error)
}
