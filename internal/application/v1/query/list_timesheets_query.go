package query

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// ListTimesheetsQuery query to list all timesheets by filter
type ListTimesheetsQuery struct {
	UserID    uint
	ProjectID uint
	Status    string
	StartDate *time.Time
	EndDate   *time.Time
}

// ListTimesheetsQueryResult query to get list result of all existing timesheets
type ListTimesheetsQueryResult struct {
	Result     []*common.TimesheetResult
	TotalCount uint
}
