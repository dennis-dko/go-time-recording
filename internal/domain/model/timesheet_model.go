package model

import "time"

const (
	TimesheetStatusOpen      = "open"
	TimesheetStatusSubmitted = "submitted"
	TimesheetStatusApproved  = "approved"
	TimesheetStatusRejected  = "rejected"
)

// Timesheet model
type Timesheet struct {
	ID            uint
	UserID        uint
	ProjectID     uint
	Date          time.Time
	DurationHours float64
	Description   *string
	Status        string
}
