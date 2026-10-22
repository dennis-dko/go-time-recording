package rest

import (
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// ExportHandler hands somebody everything held about their own account.
type ExportHandler struct {
	exports *service.AccountExportService
	authz   *Authorizer
}

// NewExportHandler creates the handler.
func NewExportHandler(exports *service.AccountExportService, authz *Authorizer) *ExportHandler {
	return &ExportHandler{exports: exports, authz: authz}
}

// Account handles GET /api/v1/me/export.
//
// Anybody signed in may ask, the built-in administrator included and whatever
// their role lets them do here: the right of access is the person's, not a
// permission. It reaches only their own account - there is no id to ask with.
func (h *ExportHandler) Account(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if !h.authz.Enabled() || principal.User.ID == 0 {
		return nil, toHTTPError(apperror.Conflictf(
			"this instance runs without authentication, so there is no account to export").
			WithCode("noAuthNoAccount"))
	}

	archive, err := h.exports.Export(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: archive, ContentType: "application/zip"}, nil
}
