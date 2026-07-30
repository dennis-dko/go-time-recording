package rest

import "github.com/dennis-dko/go-time-recording/internal/application/v1/common"

// UserResponse is the wire representation of a user.
type UserResponse struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// CreateUserRequest is the payload for creating a user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// UpdateUserRequest is the payload for a partial update: an omitted field is
// left unchanged, which is why every field is a pointer.
type UpdateUserRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Role  *string `json:"role"`
}

func newUserResponse(r *common.UserResult) UserResponse {
	return UserResponse{ID: r.ID, Name: r.Name, Email: r.Email, Role: r.Role}
}

func newUserResponses(results []*common.UserResult) []UserResponse {
	out := make([]UserResponse, 0, len(results))
	for _, r := range results {
		out = append(out, newUserResponse(r))
	}

	return out
}
