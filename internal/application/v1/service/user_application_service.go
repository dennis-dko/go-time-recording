package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// UserService service interface
type UserService interface {
	CreateUser(ctx context.Context, cmd command.CreateUserCommand) (*command.CreateUserCommandResult, error)
	GetUser(ctx context.Context, q query.GetUserQuery) (*query.GetUserQueryResult, error)
	ListUsers(ctx context.Context, q query.ListUsersQuery) (*query.ListUsersQueryResult, error)
	UpdateUser(ctx context.Context, cmd command.UpdateUserCommand) (*command.UpdateUserCommandResult, error)
	DeleteUser(ctx context.Context, cmd command.DeleteUserCommand) error
}

// UserApplicationService application service for users
type UserApplicationService struct {
	userRepository repository.UserRepository
}

// NewUserApplicationService creates new instance
func NewUserApplicationService(userRepo repository.UserRepository) *UserApplicationService {
	return &UserApplicationService{userRepository: userRepo}
}

var _ UserService = (*UserApplicationService)(nil)

// CreateUser processes the command to create a user
func (s *UserApplicationService) CreateUser(
	ctx context.Context,
	cmd command.CreateUserCommand,
) (*command.CreateUserCommandResult, error) {
	if err := validateUser(cmd.Name, cmd.Email, cmd.Role); err != nil {
		return nil, err
	}

	createdUser, err := s.userRepository.Save(ctx, &model.User{
		Name:  cmd.Name,
		Email: cmd.Email,
		Role:  cmd.Role,
	})
	if err != nil {
		return nil, err
	}

	return &command.CreateUserCommandResult{
		Result: common.NewUserResultFromModel(createdUser)[0],
	}, nil
}

// GetUser processes the query to get a user
func (s *UserApplicationService) GetUser(
	ctx context.Context,
	q query.GetUserQuery,
) (*query.GetUserQueryResult, error) {
	if q.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	user, err := s.userRepository.GetByID(ctx, q.ID)
	if err != nil {
		return nil, err
	}

	return &query.GetUserQueryResult{
		Result: common.NewUserResultFromModel(user)[0],
	}, nil
}

// ListUsers processes the query to get all users, applying optional paging.
func (s *UserApplicationService) ListUsers(
	ctx context.Context,
	q query.ListUsersQuery,
) (*query.ListUsersQueryResult, error) {
	allUsers, err := s.userRepository.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	// TotalCount reports the unpaged total so a client can size its pager.
	total := uint(len(allUsers))

	return &query.ListUsersQueryResult{
		Result:     common.NewUserResultFromModel(paginate(allUsers, q.Page, q.Limit)...),
		TotalCount: total,
	}, nil
}

// UpdateUser processes the command to update a user
func (s *UserApplicationService) UpdateUser(
	ctx context.Context,
	cmd command.UpdateUserCommand,
) (*command.UpdateUserCommandResult, error) {
	if cmd.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	existingUser, err := s.userRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	if cmd.Name != nil {
		existingUser.Name = *cmd.Name
	}

	if cmd.Email != nil {
		existingUser.Email = *cmd.Email
	}

	if cmd.Role != nil {
		existingUser.Role = *cmd.Role
	}

	if err := validateUser(existingUser.Name, existingUser.Email, existingUser.Role); err != nil {
		return nil, err
	}

	updatedUser, err := s.userRepository.Update(ctx, existingUser)
	if err != nil {
		return nil, err
	}

	return &command.UpdateUserCommandResult{
		Result: common.NewUserResultFromModel(updatedUser)[0],
	}, nil
}

// DeleteUser processes the command to delete a user
func (s *UserApplicationService) DeleteUser(ctx context.Context, cmd command.DeleteUserCommand) error {
	if cmd.ID == 0 {
		return apperror.InvalidFields("id")
	}

	return s.userRepository.Delete(ctx, cmd.ID)
}

func validateUser(name, email, role string) error {
	var invalid []string

	if name == "" {
		invalid = append(invalid, "name")
	}

	if !validEmail(email) {
		invalid = append(invalid, "email")
	}

	switch role {
	case model.UserRoleAdmin, model.UserRoleManager, model.UserRoleEmployee:
	default:
		invalid = append(invalid, "role")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}
