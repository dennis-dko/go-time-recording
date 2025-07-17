package query

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// ListUsersQuery query to list all users by filter
type ListUsersQuery struct {
	Page  int
	Limit int
}

// ListUsersQueryResult query to get list result of all existing users
type ListUsersQueryResult struct {
	Result     []*common.UserResult
	TotalCount uint
}
