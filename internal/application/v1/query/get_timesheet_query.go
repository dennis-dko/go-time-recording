package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// GetTimesheetQuery query to get existing timesheet
type GetTimesheetQuery struct {
	ID uint
}

// GetTimesheetQueryResult query to get data result of existing timesheet
type GetTimesheetQueryResult struct {
	Result *common.TimesheetResult
}
