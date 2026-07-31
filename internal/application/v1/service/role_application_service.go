package service

import (
	"context"
	"slices"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// RoleService administers roles and their permissions.
type RoleService interface {
	ListRoles(ctx context.Context) ([]*model.Role, error)
	GetRole(ctx context.Context, id uint) (*model.Role, error)
	CreateRole(ctx context.Context, name, description string, permissions []string) (*model.Role, error)
	UpdateRole(ctx context.Context, id uint, name, description *string, permissions []string) (*model.Role, error)
	DeleteRole(ctx context.Context, id uint) error
}

// RoleApplicationService application service for roles.
type RoleApplicationService struct {
	roles repository.RoleRepository
}

// NewRoleApplicationService creates new instance.
func NewRoleApplicationService(roles repository.RoleRepository) *RoleApplicationService {
	return &RoleApplicationService{roles: roles}
}

var _ RoleService = (*RoleApplicationService)(nil)

func (s *RoleApplicationService) ListRoles(ctx context.Context) ([]*model.Role, error) {
	return s.roles.GetAll(ctx)
}

func (s *RoleApplicationService) GetRole(ctx context.Context, id uint) (*model.Role, error) {
	if id == 0 {
		return nil, apperror.InvalidFields("id")
	}

	return s.roles.GetByID(ctx, id)
}

func (s *RoleApplicationService) CreateRole(
	ctx context.Context,
	name, description string,
	permissions []string,
) (*model.Role, error) {
	clean, err := validateRole(name, permissions)
	if err != nil {
		return nil, err
	}

	return s.roles.Save(ctx, &model.Role{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Permissions: clean,
	})
}

func (s *RoleApplicationService) UpdateRole(
	ctx context.Context,
	id uint,
	name, description *string,
	permissions []string,
) (*model.Role, error) {
	if id == 0 {
		return nil, apperror.InvalidFields("id")
	}

	role, err := s.roles.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// A system role may be described differently but not renamed or weakened:
	// the application looks it up by name, and stripping its permissions would
	// leave the installation unadministrable.
	if role.IsSystem {
		if name != nil && *name != role.Name {
			return nil, apperror.Conflictf("the system role %q cannot be renamed", role.Name)
		}

		if permissions != nil && !grantsAtLeast(permissions, role.Permissions) {
			return nil, apperror.Conflictf(
				"permissions cannot be removed from the system role %q", role.Name)
		}
	}

	if name != nil {
		role.Name = strings.TrimSpace(*name)
	}

	if description != nil {
		role.Description = strings.TrimSpace(*description)
	}

	if permissions != nil {
		clean, permErr := validateRole(role.Name, permissions)
		if permErr != nil {
			return nil, permErr
		}

		role.Permissions = clean
	}

	if role.Name == "" {
		return nil, apperror.InvalidFields("name")
	}

	return s.roles.Update(ctx, role)
}

func (s *RoleApplicationService) DeleteRole(ctx context.Context, id uint) error {
	if id == 0 {
		return apperror.InvalidFields("id")
	}

	role, err := s.roles.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if role.IsSystem {
		return apperror.Conflictf("the system role %q cannot be deleted", role.Name)
	}

	inUse, err := s.roles.CountUsers(ctx, id)
	if err != nil {
		return err
	}

	if inUse > 0 {
		return apperror.Conflictf("role %q is still assigned to %d user(s)", role.Name, inUse)
	}

	return s.roles.Delete(ctx, id)
}

// validateRole checks the name and rejects permissions the application does
// not enforce, so the UI cannot store a right that grants nothing.
func validateRole(name string, permissions []string) ([]string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, apperror.InvalidFields("name")
	}

	var unknown []string

	clean := make([]string, 0, len(permissions))

	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}

		if !model.IsPermission(permission) {
			unknown = append(unknown, permission)

			continue
		}

		if !slices.Contains(clean, permission) {
			clean = append(clean, permission)
		}
	}

	if len(unknown) > 0 {
		return nil, apperror.Invalidf("unknown permission(s): %s", strings.Join(unknown, ", "))
	}

	return clean, nil
}

// grantsAtLeast reports whether every permission in required is present in
// granted.
func grantsAtLeast(granted, required []string) bool {
	for _, permission := range required {
		if !slices.Contains(granted, permission) {
			return false
		}
	}

	return true
}
