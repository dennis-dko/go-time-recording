package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

	// No working times. A new account starts on the instance default and its owner
	// changes it afterwards; a daily target is a time figure, and nobody sets somebody
	// else's.
	result, err := h.users.CreateUser(c, command.CreateUserCommand{
		Name:     req.Name,
		Email:    req.Email,
		Role:     req.Role,
		Password: req.Password,
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

	// The working times are not passed on. They belong to whoever the account is
	// about, who sets them through their own working-times route; sending them here
	// was one of three ways into figures a single right was meant to guard.
	result, err := h.users.UpdateUser(c, command.UpdateUserCommand{
		ID:    id,
		Name:  req.Name,
		Email: req.Email,
		Role:  req.Role,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newUserResponse(result.Result, h.timezone.resolve(c)), nil
}

// Delete handles DELETE /api/v1/users/{id}
func (h *UserHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermUserDelete)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// Not the account doing the asking.
	//
	// Nothing about the permission stops it - somebody who may delete accounts may
	// delete every account, and theirs is one of them - and the result is a
	// request that succeeds and takes the session it arrived on with it. The
	// screen goes back to the sign-in form, the account it wants is gone, and
	// whatever else that person was in the middle of is gone with it.
	//
	// Refused rather than confirmed, because there is no version of this that is
	// what somebody meant to do from an administration screen. Somebody genuinely
	// leaving is deleted by a colleague, which is also the only way it gets
	// noticed.
	if principal.User != nil && principal.User.ID == id {
		return nil, toHTTPError(apperror.Conflictf(
			"you cannot delete the account you are signed in with").
			WithCode("cannotDeleteSelf"))
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
// Your own hours, and only your own.
//
// There was a wider permission for changing somebody else's, and it is gone: a daily
// target is a time figure, everything to do with time is the person's own, and the
// administrator cannot see what the number does anyway.
func (h *UserHandler) UpdateWorkingTimes(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if _, err := h.authz.RequireSelf(c, id, model.PermSettingsWriteOwn); err != nil {
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
