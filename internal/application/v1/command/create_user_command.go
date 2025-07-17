package command

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// CreateUserCommand command to create new user
type CreateUserCommand struct {
	Name  string
	Email string
	Role  string
}

// CreateUserCommandResult command to get create result of new user
type CreateUserCommandResult struct {
	Result *common.UserResult
}
