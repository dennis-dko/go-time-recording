package service

import (
	"context"
	"errors"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
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

// AssignRoleToUser add a new role to user
func (s *UserDomainService) AssignRoleToUser(ctx context.Context, userID uint, newRole string) (*model.User, error) {
	user, err := s.userRepository.GetByID(ctx, userID)
	if err != nil {
		return nil, errors.New("user not found")
	}

	// Check if a valid role is specified
	if !isValidRole(newRole) {
		return nil, errors.New("invalid role specified")
	}

	user.Role = newRole
	updatedUser, err := s.userRepository.Update(ctx, user)
	if err != nil {
		return nil, errors.New("failed to assign role")
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
