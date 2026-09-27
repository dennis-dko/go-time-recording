package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// GetProjectQuery query to get existing project
type GetProjectQuery struct {
	ID uint

	// ViewerID restricts access to what this user may see. Zero disables the
	// check, for callers that have already established the right.
	ViewerID uint
}

// GetProjectQueryResult query to get data result of existing project
type GetProjectQueryResult struct {
	Result *common.ProjectResult
}
