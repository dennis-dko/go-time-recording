package command

import "time"

// CreateTimesheetCommand command to create new timesheet
type CreateTimesheetCommand struct {
	UserID        uint
	ProjectID     uint
	Date          time.Time
	DurationHours float64
	Description   *string
	Status        string
}
