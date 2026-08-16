package query

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// ListTimesheetsQuery query to list all timesheets by filter
type ListTimesheetsQuery struct {
	UserID    uint
	ProjectID uint
	StartDate *time.Time
	EndDate   *time.Time

	// WithoutProject asks for the entries that belong to no project. A zero
	// ProjectID already means "any", so this is the only way to ask the other
	// question - and it is one people ask, because the hours that never got a
	// project are exactly the ones somebody goes looking for.
	WithoutProject bool
}

// ListTimesheetsQueryResult query to get list result of all existing timesheets
type ListTimesheetsQueryResult struct {
	Result     []*common.TimesheetResult
	TotalCount uint
}
