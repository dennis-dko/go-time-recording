package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// GetProjectQuery query to get existing project
type GetProjectQuery struct {
	ID uint
}

// GetProjectQueryResult query to get data result of existing project
type GetProjectQueryResult struct {
	Result *common.ProjectResult
}