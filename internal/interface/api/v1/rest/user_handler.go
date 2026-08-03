package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
)

// UserHandler serves the user endpoints.
type UserHandler struct {
	users    service.UserService
	domain   *domainservice.UserDomainService
	authz    *Authorizer
	auth     *service.AuthService
	timezone InstanceTimezoneFunc
}

// NewUserHandler creates a user handler.
func NewUserHandler(
	users service.UserService,
	domain *domainservice.UserDomainService,
	authz *Authorizer,
	auth *service.AuthService,
	timezone InstanceTimezoneFunc,
) *UserHandler {
	return &UserHandler{
		users: users, domain: domain, authz: authz, auth: auth, timezone: timezone,
	}
}

// List handles GET /api/v1/users
func (h *UserHandler) List(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserRead); err != nil {
		return nil, err
	}

	result, err := h.users.ListUsers(c, query.ListUsersQuery{
		Page:  queryInt(c, "page", 0),
		Limit: queryInt(c, "limit", 0),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return listResponse[UserResponse]{
		Items:      newUserResponses(result.Result, h.timezone.resolve(c)),
		TotalCount: result.TotalCount,
	}, nil
}

// Get handles GET /api/v1/users/{id}
func (h *UserHandler) Get(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// Anyone may look at their own account.
	if _, err := h.authz.RequireSelfOr(c, id, model.PermUserRead); err != nil {
		return nil, err
	}

	result, err := h.users.GetUser(c, query.GetUserQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result, h.timezone.resolve(c)), nil
}

// Create handles POST /api/v1/users
func (h *UserHandler) Create(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserWrite); err != nil {
		return nil, err
	}

	var req CreateUserRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.CreateUser(c, command.CreateUserCommand{
		Name:             req.Name,
		Email:            req.Email,
		Role:             req.Role,
		Password:         req.Password,
		DailyTargetHours: req.DailyTargetHours,
		MaxDailyHours:    req.MaxDailyHours,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result, h.timezone.resolve(c)), nil
}

// Update handles PUT /api/v1/users/{id}
func (h *UserHandler) Update(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserWrite); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req UpdateUserRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.UpdateUser(c, command.UpdateUserCommand{
		ID:               id,
		Name:             req.Name,
		Email:            req.Email,
		Role:             req.Role,
		DailyTargetHours: req.DailyTargetHours,
		MaxDailyHours:    req.MaxDailyHours,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result, h.timezone.resolve(c)), nil
}

// Delete handles DELETE /api/v1/users/{id}
func (h *UserHandler) Delete(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserDelete); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// ?purge=true confirms that the account's recorded time goes with it. Without
	// it an account that has booked hours is refused, and the refusal says how
	// many - so the confirmation is asked with the number in front of whoever is
	// about to agree to it.
	//
	// A query parameter rather than a body, because DELETE with a body is
	// awkward for clients and proxies alike, and this is one boolean.
	purge := boolParam(c, "purge")

	if err := h.users.DeleteUser(c, command.DeleteUserCommand{ID: id, Purge: purge}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// AssignRole handles PUT /api/v1/users/{id}/role
func (h *UserHandler) AssignRole(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermUserWrite); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req AssignRoleRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	user, err := h.domain.AssignRoleToUser(c, id, req.Role)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponseFromModel(user, h.timezone.resolve(c)), nil
}

// UpdateWorkingTimes handles PUT /api/v1/users/{id}/working-times.
//
// Users may set their own hours; changing someone else's needs the wider
// permission.
func (h *UserHandler) UpdateWorkingTimes(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if _, err := h.authz.RequireSelfOr(c, id, model.PermSettingsWriteOther); err != nil {
		return nil, err
	}

	var req WorkingTimesRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.users.UpdateWorkingTimes(c, command.UpdateWorkingTimesCommand{
		ID:               id,
		DailyTargetHours: req.DailyTargetHours,
		MaxDailyHours:    req.MaxDailyHours,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result, h.timezone.resolve(c)), nil
}
