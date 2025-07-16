package command

import "time"

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

// ChangeTimesheetStatusCommand command to change the status of an existing timesheet
type ChangeTimesheetStatusCommand struct {
	ID        uint
	NewStatus string
}
