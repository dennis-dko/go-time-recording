package command

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// CreateUserCommand command to create new user
type CreateUserCommand struct {
	Name  string
	Email string

	// Role is the role name. Empty falls back to the least privileged role.
	Role string

	// Password may be empty, in which case the account is created with the
	// initial password and must change it on first use.
	Password string

	// Working times; zero means "use the instance default".
	DailyTargetHours float64
	MaxDailyHours    float64
}

// CreateUserCommandResult command to get create result of new user
type CreateUserCommandResult struct {
	Result *common.UserResult
}
