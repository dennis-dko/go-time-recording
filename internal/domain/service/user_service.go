package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// UserDomainService encapsulates domain logic
type UserDomainService struct {
	userRepository repository.UserRepository
}

// NewUserDomainService creates new instance
func NewUserDomainService(userRepo repository.UserRepository) *UserDomainService {
	return &UserDomainService{
		userRepository: userRepo,
	}
}

// AssignRoleToUser assigns a new role to a user.
func (s *UserDomainService) AssignRoleToUser(
	ctx context.Context,
	userID uint,
	newRole string,
) (*model.User, error) {
	user, err := s.userRepository.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if !isValidRole(newRole) {
		return nil, apperror.InvalidFields("role")
	}

	user.Role = newRole

	updatedUser, err := s.userRepository.Update(ctx, user)
	if err != nil {
		return nil, err
	}

	return updatedUser, nil
}

// isValidRole validates if the role exist
func isValidRole(role string) bool {
	switch role {
	case model.UserRoleAdmin, model.UserRoleEmployee, model.UserRoleManager:
		return true
	default:
		return false
	}
}
