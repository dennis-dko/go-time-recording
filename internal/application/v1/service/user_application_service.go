package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
)

// UserService service interface
type UserService interface {
	CreateUser(ctx context.Context, cmd command.CreateUserCommand) (*command.CreateUserCommandResult, error)
	GetUser(ctx context.Context, q query.GetUserQuery) (*query.GetUserQueryResult, error)
	ListUsers(ctx context.Context, q query.ListUsersQuery) (*query.ListUsersQueryResult, error)
	UpdateUser(ctx context.Context, cmd command.UpdateUserCommand) (*command.UpdateUserCommandResult, error)
	DeleteUser(ctx context.Context, cmd command.DeleteUserCommand) error
}
