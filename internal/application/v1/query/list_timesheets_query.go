package query

import "time"

// ListTimesheetsQuery query to list all timesheets by filter
type ListTimesheetsQuery struct {
	UserID    uint
	ProjectID uint
	Status    string
	StartDate *time.Time
	EndDate   *time.Time
}
