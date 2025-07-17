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
	Status        *string
}

// UpdateTimesheetCommandResult command to get update result of existing timesheet
type UpdateTimesheetCommandResult struct {
	Result *common.TimesheetResult
}
