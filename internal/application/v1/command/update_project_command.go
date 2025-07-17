package command

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// UpdateProjectCommand command to update existing project
type UpdateProjectCommand struct {
	ID          uint
	Name        *string
	Description *string
	StartDate   *time.Time
	EndDate     *time.Time
	Status      *string
}

// UpdateProjectCommandResult command to get update result of existing project
type UpdateProjectCommandResult struct {
	Result *common.ProjectResult
}
