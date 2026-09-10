package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// UserDomainService holds the one rule about moving an account between roles:
// the built-in administrator keeps a role that can still administer.
type UserDomainService struct {
	userRepository repository.UserRepository
	roleRepository repository.RoleRepository
}

// NewUserDomainService needs the roles to see what the one an account is moved
// to may do.
func NewUserDomainService(
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
) *UserDomainService {
	return &UserDomainService{
		userRepository: userRepo,
		roleRepository: roleRepo,
	}
}

// AssignRoleToUser moves a user to the named role.
func (s *UserDomainService) AssignRoleToUser(
	ctx context.Context,
	userID uint,
	roleName string,
) (*model.User, error) {
	user, err := s.userRepository.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	role, err := s.roleRepository.GetByName(ctx, roleName)
	if err != nil {
		return nil, err
	}

	// The built-in administrator must keep a role that can still administer,
	// otherwise an installation can be locked out of its own user management.
	if user.IsSystem && !role.Has(model.PermRoleWrite) {
		return nil, apperror.Conflictf(
			"the built-in administrator cannot be moved to role %q, which lacks %q",
			role.Name, model.PermRoleWrite).
			WithCode("adminRoleMustAdminister", role.Name, model.PermRoleWrite)
	}

	user.RoleID = role.ID

	updatedUser, err := s.userRepository.Update(ctx, user)
	if err != nil {
		return nil, err
	}

	return updatedUser, nil
}
