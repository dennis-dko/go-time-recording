package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
)

// UserHandler serves the user endpoints.
type UserHandler struct {
	users  service.UserService
	domain *domainservice.UserDomainService
}

// NewUserHandler creates a user handler.
func NewUserHandler(users service.UserService, domain *domainservice.UserDomainService) *UserHandler {
	return &UserHandler{users: users, domain: domain}
}

// List handles GET /api/v1/users
func (h *UserHandler) List(c *gofr.Context) (any, error) {
	result, err := h.users.ListUsers(c, query.ListUsersQuery{
		Page:  queryInt(c, "page", 0),
		Limit: queryInt(c, "limit", 0),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return listResponse[UserResponse]{
		Items:      newUserResponses(result.Result),
		TotalCount: result.TotalCount,
	}, nil
}

// Get handles GET /api/v1/users/{id}
func (h *UserHandler) Get(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.GetUser(c, query.GetUserQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result), nil
}

// Create handles POST /api/v1/users
func (h *UserHandler) Create(c *gofr.Context) (any, error) {
	var req CreateUserRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.CreateUser(c, command.CreateUserCommand{
		Name:  req.Name,
		Email: req.Email,
		Role:  req.Role,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result), nil
}

// Update handles PUT /api/v1/users/{id}
func (h *UserHandler) Update(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req UpdateUserRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.UpdateUser(c, command.UpdateUserCommand{
		ID:    id,
		Name:  req.Name,
		Email: req.Email,
		Role:  req.Role,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result), nil
}

// Delete handles DELETE /api/v1/users/{id}
func (h *UserHandler) Delete(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.users.DeleteUser(c, command.DeleteUserCommand{ID: id}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// AssignRole handles PUT /api/v1/users/{id}/role, exposing the domain rule
// that validates the role before applying it.
func (h *UserHandler) AssignRole(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req struct {
		Role string `json:"role"`
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	user, err := h.domain.AssignRoleToUser(c, id, req.Role)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return UserResponse{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role}, nil
}
