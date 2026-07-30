package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
)

// ProjectHandler serves the project endpoints.
type ProjectHandler struct {
	projects service.ProjectService
	domain   *domainservice.ProjectDomainService
}

// NewProjectHandler creates a project handler.
func NewProjectHandler(
	projects service.ProjectService,
	domain *domainservice.ProjectDomainService,
) *ProjectHandler {
	return &ProjectHandler{projects: projects, domain: domain}
}

// List handles GET /api/v1/projects
func (h *ProjectHandler) List(c *gofr.Context) (any, error) {
	result, err := h.projects.ListProjects(c, query.ListProjectsQuery{Status: c.Param("status")})
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
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	result, err := h.projects.GetProject(c, query.GetProjectQuery{ID: id})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newProjectResponse(result.Result), nil
}

// Create handles POST /api/v1/projects
func (h *ProjectHandler) Create(c *gofr.Context) (any, error) {
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

// Update handles PUT /api/v1/projects/{id}
func (h *ProjectHandler) Update(c *gofr.Context) (any, error) {
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

// Delete handles DELETE /api/v1/projects/{id}
func (h *ProjectHandler) Delete(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.projects.DeleteProject(c, command.DeleteProjectCommand{ID: id}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// Archive handles POST /api/v1/projects/{id}/archive, delegating to the domain
// service that owns the archiving preconditions.
func (h *ProjectHandler) Archive(c *gofr.Context) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	project, err := h.domain.ArchiveProject(c, id)
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
