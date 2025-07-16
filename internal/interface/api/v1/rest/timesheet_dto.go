package rest

import "github.com/dennis-dko/go-time-recording/internal/domain/model"

// Timesheet data transfer object
type Timesheet struct {
	model.Timesheet
}

// RestPath define timesheet api endpoint
func (t *Timesheet) RestPath() string {
	return "api/v1/timesheets"
}
