package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// RoleHandler serves the role administration endpoints.
type RoleHandler struct {
	roles service.RoleService
	authz *Authorizer
	auth  *service.AuthService
}

// NewRoleHandler creates a role handler.
func NewRoleHandler(roles service.RoleService, authz *Authorizer, auth *service.AuthService) *RoleHandler {
	return &RoleHandler{roles: roles, authz: authz, auth: auth}
}

// List handles GET /api/v1/roles
func (h *RoleHandler) List(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleRead); err != nil {
		return nil, err
	}

	roles, err := h.roles.ListRoles(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]RoleResponse, 0, len(roles))
	for _, role := range roles {
		items = append(items, newRoleResponse(role))
	}

	return listResponse[RoleResponse]{Items: items, TotalCount: uint(len(items))}, nil
}

// Permissions handles GET /api/v1/permissions, listing every permission the
// application enforces so the UI can offer exactly those.
func (h *RoleHandler) Permissions(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleRead); err != nil {
		return nil, err
	}

	return map[string][]string{"permissions": model.AllPermissions()}, nil
}

// Get handles GET /api/v1/roles/{id}
func (h *RoleHandler) Get(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleRead); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	role, err := h.roles.GetRole(c, id)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newRoleResponse(role), nil
}

// Create handles POST /api/v1/roles
func (h *RoleHandler) Create(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleWrite); err != nil {
		return nil, err
	}

	var req RoleRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if req.Name == nil {
		return nil, toHTTPError(apperror.InvalidFields("name"))
	}

	role, err := h.roles.CreateRole(c, *req.Name, valueOr(req.Description, ""), req.Permissions)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newRoleResponse(role), nil
}

// Update handles PUT /api/v1/roles/{id}
func (h *RoleHandler) Update(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleWrite); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	var req RoleRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	role, err := h.roles.UpdateRole(c, id, req.Name, req.Description, req.Permissions)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newRoleResponse(role), nil
}

// Delete handles DELETE /api/v1/roles/{id}
func (h *RoleHandler) Delete(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermRoleWrite); err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.roles.DeleteRole(c, id); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

func valueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}

	return *p
}
