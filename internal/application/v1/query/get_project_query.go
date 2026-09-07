// Package query holds the read requests the application layer accepts, and the
// results they come back as.
//
// Separate from command because a read carries a question a write does not: who
// is asking. GetProjectQuery and ListProjectsQuery carry a ViewerID for exactly
// that, and it decides visibility rather than filtering - a project the viewer
// may not see is answered as not found, not as refused. Zero disables the
// check, for callers that have already established the right.
//
// Where a query also names whose records are wanted, that is a second and
// different id: ListTimesheetsQuery's UserID says whose hours to total, which
// is not the same question as whether a project may be seen. They are separate
// fields here and never one, because one id doing both jobs is how a total ends
// up including somebody else's work.
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
