package command

// DeleteUserCommand command to delete existing user
type DeleteUserCommand struct {
	ID uint

	// Purge confirms that the account's recorded time is to be destroyed with
	// it.
	//
	// Without it an account that has booked hours is refused rather than
	// emptied. The hours are the only thing here that cannot be recreated - an
	// account can be added again in a minute - so removing them is a decision
	// somebody has to make on purpose rather than a side effect of a click.
	Purge bool
}
