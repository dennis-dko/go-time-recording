package command

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// UpdateUserCommand command to update existing user
type UpdateUserCommand struct {
	ID    uint
	Name  *string
	Email *string
	Role  *string
}

// UpdateUserCommandResult command to get update result of existing user
type UpdateUserCommandResult struct {
	Result *common.UserResult
}
