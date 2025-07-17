package common

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TimesheetResult result model
type TimesheetResult struct {
	ID            uint
	UserID        uint
	ProjectID     uint
	Date          time.Time
	DurationHours float64
	Description   *string
	Status        string
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
			Status:        timesheetModel.Status,
		}
		if timesheetModel.Description != nil {
			timesheetData.Description = timesheetModel.Description
		}
		timesheetResult = append(timesheetResult, timesheetData)
	}
	return timesheetResult
}
