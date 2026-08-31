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

	// Limit is the page size and Offset how many matching entries to skip. A zero
	// Limit reads everything, which is what the exports and the evaluations want;
	// the listing that answers a screen always names one.
	Limit  uint
	Offset uint
}

// ListTimesheetsQueryResult query to get list result of all existing timesheets
type ListTimesheetsQueryResult struct {
	Result []*common.TimesheetResult

	// TotalCount is how many entries match, not how many came back. Those differ
	// exactly when there is another page, which is the one thing a caller cannot
	// work out for itself.
	TotalCount uint
}
