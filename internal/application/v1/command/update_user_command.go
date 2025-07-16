package command

// UpdateUserCommand command to update existing user
type UpdateUserCommand struct {
	ID    uint
	Name  *string
	Email *string
	Role  *string
}
