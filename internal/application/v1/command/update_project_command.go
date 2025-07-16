package command

import "time"

// UpdateProjectCommand command to update existing project
type UpdateProjectCommand struct {
	ID          uint
	Name        *string
	Description *string
	StartDate   *time.Time
	EndDate     *time.Time
	Status      *string
}
