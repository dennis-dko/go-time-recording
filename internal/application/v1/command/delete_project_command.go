package command

// DeleteProjectCommand command to delete existing project
type DeleteProjectCommand struct {
	ID uint

	// ActorID is who is deleting. A private project may only be removed by
	// its owner; zero skips the check.
	ActorID uint
}
