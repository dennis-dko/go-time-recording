package common

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TimesheetResult result model
type TimesheetResult struct {
	ID     uint
	UserID uint

	// ProjectID is nil for an entry that has not been assigned to a project.
	ProjectID *uint

	Date          time.Time
	DurationHours float64
	Description   *string

	// CreatedAt and UpdatedAt are zero for an entry booked before the audit
	// columns existed. See model.Timesheet: zero is "nobody recorded this", not a
	// moment in 1970, and the wire representation turns it into null rather than
	// into a date.
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewTimesheetResultFromModel(timesheetModels ...*model.Timesheet) []*TimesheetResult {
	if timesheetModels == nil {
		return nil
	}
	var timesheetResult []*TimesheetResult
	for _, timesheetModel := range timesheetModels {
		timesheetData := &TimesheetResult{
			ID:            timesheetModel.ID,
			UserID:        timesheetModel.UserID,
			ProjectID:     timesheetModel.ProjectID,
			Date:          timesheetModel.Date,
			DurationHours: timesheetModel.DurationHours,
			Description:   timesheetModel.Description,
			CreatedAt:     timesheetModel.CreatedAt,
			UpdatedAt:     timesheetModel.UpdatedAt,
		}
		if timesheetModel.Description != nil {
			timesheetData.Description = timesheetModel.Description
		}
		timesheetResult = append(timesheetResult, timesheetData)
	}
	return timesheetResult
}
