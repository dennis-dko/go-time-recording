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
	ID            uint      `json:"id"  sql:"auto_increment"`
	UserID        uint      `json:"userId" sql:"not_null"`
	ProjectID     uint      `json:"projectId" sql:"not_null"`
	Date          time.Time `json:"date" sql:"not_null"`
	DurationHours float64   `json:"durationHours" sql:"not_null"`
	Description   *string   `json:"description,omitempty"`
	Status        string    `json:"status" sql:"not_null"`
}

func (t *Timesheet) TableName() string {
	return "timesheet"
}
