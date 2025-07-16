package rest

import "gofr.dev/pkg/gofr"

// GetAll get all timehseets
func (t *Timesheet) GetAll(c *gofr.Context) (any, error) {
	return "timesheet GetAll called", nil
}
