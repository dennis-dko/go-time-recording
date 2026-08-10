package rest

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// ProjectResponse is the wire representation of a project.
type ProjectResponse struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	StartDate   Date    `json:"startDate"`
	EndDate     *Date   `json:"endDate"`
	Status      string  `json:"status"`

	// OwnerID is whose project it is. There was a "private" beside it, true for a
	// personal category and false for a shared project; there is one kind now, so it
	// would always be true and would tell a reader nothing.
	OwnerID *uint `json:"ownerId"`
}

// CreateProjectRequest is the payload for creating a project.
type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	StartDate   Date    `json:"startDate"`
	EndDate     *Date   `json:"endDate"`
	Status      string  `json:"status"`
}

// UpdateProjectRequest is the payload for a partial update.
type UpdateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	StartDate   *Date   `json:"startDate"`
	EndDate     *Date   `json:"endDate"`
	Status      *string `json:"status"`
}

func newProjectResponse(r *common.ProjectResult) ProjectResponse {
	resp := ProjectResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		StartDate:   Date{Time: r.StartDate},
		Status:      r.Status,
		OwnerID:     r.OwnerID,
	}

	if r.EndDate != nil {
		resp.EndDate = &Date{Time: *r.EndDate}
	}

	return resp
}

func newProjectResponses(results []*common.ProjectResult) []ProjectResponse {
	out := make([]ProjectResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newProjectResponse(r))
	}

	return out
}
