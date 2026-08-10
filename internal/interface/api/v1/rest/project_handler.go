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
// Your own projects, which is all there are. There were two kinds and a
// "private=true" parameter to tell them apart; every project belongs to one person
// now, so there is nothing to narrow and the parameter is gone.
func (h *ProjectHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectRead)
	if err != nil {
		return nil, err
	}

	result, err := h.projects.ListProjects(c, query.ListProjectsQuery{
		Status:   c.Param("status"),
		ViewerID: h.viewerID(principal),
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
	principal, err := h.authz.Require(c, model.PermProjectRead)
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

// requireMayWrite was here, to decide between two rights: a shared project needed
// projects:write and your own category did not. There is one kind of project now, so
// there is one right - and whether it is yours is the service's own check, which
// reports somebody else's as absent rather than as forbidden so its existence stays
// private.

// Create handles POST /api/v1/projects.
//
// The project belongs to whoever created it, always. There was a "private": true to
// ask for that, and projects:write against projects:write:own to decide which kind
// somebody was allowed to make; there is one kind now, so there is nothing to ask
// for and nothing to choose between.
func (h *ProjectHandler) Create(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectWrite)
	if err != nil {
		return nil, err
	}

	var req CreateProjectRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	cmd := command.CreateProjectCommand{
		Name:        req.Name,
		Description: req.Description,
		StartDate:   req.StartDate.Time,
		Status:      req.Status,
	}

	// Whoever is asking. Zero means enforcement is switched off, and then there is no
	// "whose" to record - the same reading every other handler takes, and the reads
	// short-circuit their visibility check in that case too.
	if owner := h.viewerID(principal); owner != 0 {
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
// Yours to change; the service refuses anything that is not, and reports it as absent
// rather than as forbidden so its existence stays private.
func (h *ProjectHandler) Update(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermProjectWrite)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
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
	principal, err := h.authz.Require(c, model.PermProjectDelete)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	err = h.projects.DeleteProject(c, command.DeleteProjectCommand{
		ID: id, ActorID: h.viewerID(principal),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// requireMayDelete was here, beside requireMayWrite, letting the owner of a private
// category remove it without holding projects:delete. Both are gone for the same
// reason: there is one kind of project, so one right decides, and whether it is yours
// is the service's own check.

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
