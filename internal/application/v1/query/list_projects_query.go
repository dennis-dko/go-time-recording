package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// ListProjectsQuery query to list all projects by filter
type ListProjectsQuery struct {
	Status string

	// ViewerID scopes the result to what this user may see, which is their own
	// projects and nothing else. Zero means no scoping.
	//
	// OnlyOwn was here, to narrow a list of everybody's shared projects down to the
	// viewer's private ones. There is nothing left to narrow: every project is
	// somebody's own, so the scoping above is the whole answer.
	ViewerID uint
}

// ListProjectsQueryResult query to get list result of all existing projects
type ListProjectsQueryResult struct {
	Result     []*common.ProjectResult
	TotalCount uint
}
