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

	// BudgetHours is how many hours the owner means to spend on it, nil for none.
	BudgetHours *float64

	// Billable says whether its hours are charged to somebody; HourlyRate is what
	// an hour is charged at, in whole units of Currency, nil or zero for none yet;
	// and an empty Currency is the default one.
	Billable   bool
	HourlyRate *float64
	Currency   string
}

// CreateProjectCommandResult command to get create result of new project
type CreateProjectCommandResult struct {
	Result *common.ProjectResult
}
