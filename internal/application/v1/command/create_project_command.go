package command

import "time"

// CreateProjectCommand command to create new project
type CreateProjectCommand struct {
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string
}
