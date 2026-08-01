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
	ID     uint
	UserID uint

	// ProjectID is optional: hours can be recorded first and categorised
	// later, or left uncategorised entirely.
	ProjectID *uint

	Date          time.Time
	DurationHours float64
	Description   *string
	Status        string
}

// HasProject reports whether the entry is assigned to a project.
func (t *Timesheet) HasProject() bool {
	return t.ProjectID != nil && *t.ProjectID != 0
}
