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

	// OwnerID is whose the project is. nil only where authentication is switched
	// off and there is nobody to record.
	OwnerID *uint

	// Today is the creator's own date, which a project given no start begins on.
	// The caller reads it in the creator's zone, because only the caller knows
	// that zone: the server's clock put a project created in the evening west of
	// UTC on the next day.
	Today time.Time
}

// CreateProjectCommandResult command to get create result of new project
type CreateProjectCommandResult struct {
	Result *common.ProjectResult
}
