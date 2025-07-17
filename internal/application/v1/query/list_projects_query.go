package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// ListProjectsQuery query to list all projects by filter
type ListProjectsQuery struct {
	Status string
}

// ListProjectsQueryResult query to get list result of all existing projects
type ListProjectsQueryResult struct {
	Result     []*common.ProjectResult
	TotalCount uint
}
