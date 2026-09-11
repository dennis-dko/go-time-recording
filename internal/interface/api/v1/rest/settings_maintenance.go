package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
)

// MaintenanceResponse is the maintenance state on the wire.
type MaintenanceResponse struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

// Maintenance handles GET /api/v1/maintenance.
//
// Public, like the branding, and for the same reason: somebody who opens the page
// during maintenance should read the notice on the sign-in screen rather than
// watch requests fail silently. It reveals that an installation is down for
// maintenance, which is what a maintenance notice is for.
func (h *SettingsHandler) Maintenance(c *gofr.Context) (any, error) {
	maintenance, err := h.settings.Maintenance(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return MaintenanceResponse{Enabled: maintenance.Enabled, Message: maintenance.Message}, nil
}

// SaveMaintenance handles PUT /api/v1/settings/maintenance.
func (h *SettingsHandler) SaveMaintenance(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req MaintenanceResponse
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	saved := model.Maintenance{Enabled: req.Enabled, Message: req.Message}

	if err := h.settings.SaveMaintenance(c, saved); err != nil {
		return nil, toHTTPError(err)
	}

	if h.maintenance != nil {
		h.maintenance.Invalidate()
	}

	// Every open screen, at once.
	//
	// Nothing an idle screen asks for would tell it. Who you are, what this
	// installation is called, whether it is out of service - all of those keep
	// answering during maintenance on purpose, so a browser nobody is touching
	// went on looking like a working application until its next click, or until
	// the once-a-minute permission poll came round. The stream this uses is
	// already open by then, which is the only reason it can be reached at all
	// once the door is shut behind it.
	if h.announcements != nil {
		h.announcements.Publish(announce.Maintenance, "")
	}

	// Read back rather than echo: the message is trimmed and cut on the way in,
	// so echoing the request would show the administrator something the
	// installation is not going to say.
	stored, err := h.settings.Maintenance(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return MaintenanceResponse{Enabled: stored.Enabled, Message: stored.Message}, nil
}
