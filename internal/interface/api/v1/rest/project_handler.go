package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
)

// ProjectHandler serves the project endpoints.
type ProjectHandler struct {
	projects service.ProjectService
	domain   *domainservice.ProjectDomainService
	authz    *Authorizer
}

// NewProjectHandler creates a project handler.
func NewProjectHandler(
	projects service.ProjectService,
	domain *domainservice.ProjectDomainService,
	authz *Authorizer,
) *ProjectHandler {
	return &ProjectHandler{projects: projects, domain: domain, authz: authz}
}

// List handles GET /api/v1/projects.
//
// Everyone sees the shared projects plus their own private categories; a
// "private=true" parameter narrows the result to the latter.
func (h *ProjectHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermProjectRead, model.PermProjectWriteOwn)
	if err != nil {
		return nil, err
	}

	result, err := h.projects.ListProjects(c, query.ListProjectsQuery{
		Status:   c.Param("status"),
		ViewerID: h.viewerID(principal),
		OnlyOwn:  c.Param("private") == "true",
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return listResponse[ProjectResponse]{
		Items:      newProjectResponses(result.Result),
		TotalCount: result.TotalCount,
	}, nil
}

// Get handles GET /api/v1/projects/{id}
func (h *ProjectHandler) Get(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermProjectRead, model.PermProjectWriteOwn)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.projects.GetProject(c, query.GetProjectQuery{
		ID: id, ViewerID: h.viewerID(principal),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newProjectResponse(result.Result), nil
}

// viewerID returns the id the visibility rules should be evaluated against,
// or 0 when authorization is switched off entirely.
func (h *ProjectHandler) viewerID(principal *service.Principal) uint {
	if !h.authz.Enabled() || principal.User == nil {
		return 0
	}

	return principal.User.ID
}

// requireMayWrite decides whether the caller may change this particular
// project: a shared one needs projects:write, an own category does not.
func (h *ProjectHandler) requireMayWrite(
	c *gofr.Context,
	principal *service.Principal,
	projectID uint,
) error {
	if !h.authz.Enabled() || principal.Can(model.PermProjectWrite) {
		return nil
	}

	existing, err := h.projects.GetProject(c, query.GetProjectQuery{
		ID: projectID, ViewerID: h.viewerID(principal),
	})
	if err != nil {
		return toHTTPError(err)
	}

	if existing.Result.OwnerID != nil && *existing.Result.OwnerID == principal.User.ID {
		return nil
	}

	return forbiddenError{msg: "missing permission: " + model.PermProjectWrite}
}

// Create handles POST /api/v1/projects.
//
// Sending "private": true creates a personal category, which only needs the
// own-project permission. A shared project still needs projects:write.
func (h *ProjectHandler) Create(c *gofr.Context) (any, error) {
	var req CreateProjectRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	permission := model.PermProjectWrite
	if req.Private {
		permission = model.PermProjectWriteOwn
	}

	principal, err := h.authz.Require(c, permission)
	if err != nil {
		return nil, err
	}

	cmd := command.CreateProjectCommand{
		Name:        req.Name,
		Description: req.Description,
		StartDate:   req.StartDate.Time,
		Status:      req.Status,
	}

	if req.Private && principal.User != nil && principal.User.ID != 0 {
		owner := principal.User.ID
		cmd.OwnerID = &owner
	}

	if req.EndDate != nil {
		end := req.EndDate.Time
		cmd.EndDate = &end
	}

	result, err := h.projects.CreateProject(c, cmd)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newProjectResponse(result.Result), nil
}

// Update handles PUT /api/v1/projects/{id}.
//
// Editing your own category needs only the own-project permission; the
// service refuses anything that is not yours.
func (h *ProjectHandler) Update(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermProjectWrite, model.PermProjectWriteOwn)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.requireMayWrite(c, principal, id); err != nil {
		return nil, err
	}

	var req UpdateProjectRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	cmd := command.UpdateProjectCommand{
		ID:          id,
		ActorID:     h.viewerID(principal),
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
	}

	if req.StartDate != nil {
		start := req.StartDate.Time
		cmd.StartDate = &start
	}

	if req.EndDate != nil {
		end := req.EndDate.Time
		cmd.EndDate = &end
	}

	result, err := h.projects.UpdateProject(c, cmd)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newProjectResponse(result.Result), nil
}

// Delete handles DELETE /api/v1/projects/{id}.
//
// Removing your own category needs only the own-project permission; deleting
// a shared project still needs projects:delete.
func (h *ProjectHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.RequireAny(c, model.PermProjectDelete, model.PermProjectWriteOwn)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.requireMayDelete(c, principal, id); err != nil {
		return nil, err
	}

	err = h.projects.DeleteProject(c, command.DeleteProjectCommand{
		ID: id, ActorID: h.viewerID(principal),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// requireMayDelete mirrors requireMayWrite for removal.
func (h *ProjectHandler) requireMayDelete(
	c *gofr.Context,
	principal *service.Principal,
	projectID uint,
) error {
	if !h.authz.Enabled() || principal.Can(model.PermProjectDelete) {
		return nil
	}

	existing, err := h.projects.GetProject(c, query.GetProjectQuery{
		ID: projectID, ViewerID: h.viewerID(principal),
	})
	if err != nil {
		return toHTTPError(err)
	}

	if existing.Result.OwnerID != nil && *existing.Result.OwnerID == principal.User.ID {
		return nil
	}

	return forbiddenError{msg: "missing permission: " + model.PermProjectDelete}
}

// Archive handles POST /api/v1/projects/{id}/archive, delegating to the domain
// service that owns the archiving preconditions.
func (h *ProjectHandler) Archive(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectArchive)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	project, err := h.domain.ArchiveProject(c, id, h.viewerID(principal))
	if err != nil {
		return nil, toHTTPError(err)
	}

	resp := ProjectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		StartDate:   Date{Time: project.StartDate},
		Status:      project.Status,
	}

	if project.EndDate != nil {
		resp.EndDate = &Date{Time: *project.EndDate}
	}

	return resp, nil
}
