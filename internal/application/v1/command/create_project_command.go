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
}

// CreateProjectCommandResult command to get create result of new project
type CreateProjectCommandResult struct {
	Result *common.ProjectResult
}
