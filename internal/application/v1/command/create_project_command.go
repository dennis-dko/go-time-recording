package command

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// CreateProjectCommand command to create new project
type CreateProjectCommand struct {
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string

	// OwnerID makes the project private to that user, where it serves as a
	// personal category. nil creates a shared project.
	OwnerID *uint
}

// CreateProjectCommandResult command to get create result of new project
type CreateProjectCommandResult struct {
	Result *common.ProjectResult
}
