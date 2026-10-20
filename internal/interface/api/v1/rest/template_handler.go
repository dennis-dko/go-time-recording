package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TemplateHandler lets somebody keep the bookings they make often, to fill the
// booking form with.
//
// Under /me/ and behind the right to book one's own time: a template is only ever
// a way to fill that form, so somebody who cannot book has no use for one, and
// nobody keeps them for anybody else.
type TemplateHandler struct {
	templates *service.TemplateService
	authz     *Authorizer
}

// NewTemplateHandler creates the handler.
func NewTemplateHandler(templates *service.TemplateService, authz *Authorizer) *TemplateHandler {
	return &TemplateHandler{templates: templates, authz: authz}
}

// TemplateResponse is one template. Each of its three parts is null where it
// fills nothing in.
type TemplateResponse struct {
	ID            uint     `json:"id"`
	ProjectID     *uint    `json:"projectId"`
	DurationHours *float64 `json:"durationHours"`
	Description   *string  `json:"description"`
}

// TemplateRequest is a template to keep: any of the three, at least one of them.
type TemplateRequest struct {
	ProjectID     *uint    `json:"projectId"`
	DurationHours *float64 `json:"durationHours"`
	Description   *string  `json:"description"`
}

func newTemplateResponse(template model.EntryTemplate) TemplateResponse {
	return TemplateResponse{
		ID:            template.ID,
		ProjectID:     template.ProjectID,
		DurationHours: template.DurationHours,
		Description:   template.Description,
	}
}

// List handles GET /api/v1/me/templates.
func (h *TemplateHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	kept, err := h.templates.Templates(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	items := make([]TemplateResponse, 0, len(kept))
	for _, template := range kept {
		items = append(items, newTemplateResponse(template))
	}

	return listResponse[TemplateResponse]{Items: items, TotalCount: uint(len(items))}, nil
}

// Save handles POST /api/v1/me/templates, which keeps a template - or answers
// with the one already kept that fills the form the same way.
func (h *TemplateHandler) Save(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	var req TemplateRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	kept, err := h.templates.Keep(c, model.EntryTemplate{
		UserID:        principal.User.ID,
		ProjectID:     req.ProjectID,
		DurationHours: req.DurationHours,
		Description:   req.Description,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newTemplateResponse(*kept), nil
}

// Delete handles DELETE /api/v1/me/templates/{id}.
func (h *TemplateHandler) Delete(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	id, err := pathID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.templates.Remove(c, id, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}
