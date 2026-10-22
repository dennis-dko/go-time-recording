package command

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// UpdateTimesheetCommand command to update existing timesheet
type UpdateTimesheetCommand struct {
	ID            uint
	UserID        *uint
	ProjectID     *uint
	Date          *time.Time
	DurationHours *float64
	Description   *string

	// UpdatedBy is the manager making the change in the owner's name, zero for the
	// owner.
	UpdatedBy uint
}

// UpdateTimesheetCommandResult command to get update result of existing timesheet
type UpdateTimesheetCommandResult struct {
	Result *common.TimesheetResult
}
