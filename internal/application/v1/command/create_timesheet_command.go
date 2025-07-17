package command

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// CreateTimesheetCommand command to create new timesheet
type CreateTimesheetCommand struct {
	UserID        uint
	ProjectID     uint
	Date          time.Time
	DurationHours float64
	Description   *string
	Status        string
}

// CreateTimesheetCommandResult command to get create result of new timesheet
type CreateTimesheetCommandResult struct {
	Result *common.TimesheetResult
}
