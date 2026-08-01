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

	// Private marks a personal category, visible only to its owner.
	Private bool  `json:"private"`
	OwnerID *uint `json:"ownerId"`
}

// CreateProjectRequest is the payload for creating a project.
type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	StartDate   Date    `json:"startDate"`
	EndDate     *Date   `json:"endDate"`
	Status      string  `json:"status"`

	// Private creates a personal category owned by the caller instead of a
	// shared project. It needs only the own-project permission.
	Private bool `json:"private"`
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
		Private:     r.OwnerID != nil,
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
