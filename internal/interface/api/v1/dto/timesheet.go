package dto

// Timesheet data transfer object
type Timesheet struct {
	UserID        string  `json:"userId"`
	ProjectID     string  `json:"projectId"`
	Date          string  `json:"date"`
	DurationHours float64 `json:"durationHours"`
	Description   *string `json:"description,omitempty"`
}

func (t *Timesheet) RestPath() string {
	return "api/v1/timesheets"
}
