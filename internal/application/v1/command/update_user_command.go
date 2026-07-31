package command

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// UpdateUserCommand command to update existing user
type UpdateUserCommand struct {
	ID    uint
	Name  *string
	Email *string

	// Role is the role name to move the user to.
	Role *string

	DailyTargetHours *float64
	MaxDailyHours    *float64
}

// UpdateUserCommandResult command to get update result of existing user
type UpdateUserCommandResult struct {
	Result *common.UserResult
}

// UpdateWorkingTimesCommand command to change only a user's working times.
// Separate from UpdateUserCommand so a user can adjust their own hours
// without holding the right to edit accounts.
type UpdateWorkingTimesCommand struct {
	ID               uint
	DailyTargetHours *float64
	MaxDailyHours    *float64
}
