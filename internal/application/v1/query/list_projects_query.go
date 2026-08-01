package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// ListProjectsQuery query to list all projects by filter
type ListProjectsQuery struct {
	Status string

	// ViewerID scopes the result to what this user may see: all shared
	// projects plus their own private ones. Zero means no scoping.
	ViewerID uint

	// OnlyOwn narrows the result to the viewer's private projects.
	OnlyOwn bool
}

// ListProjectsQueryResult query to get list result of all existing projects
type ListProjectsQueryResult struct {
	Result     []*common.ProjectResult
	TotalCount uint
}
