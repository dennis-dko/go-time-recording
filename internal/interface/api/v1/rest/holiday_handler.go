package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// HolidayHandler answers whose public holidays apply: the installation's, set by
// whoever administers it, and each person's own, set by them.
//
// The two halves of one question, which is why they share a handler: the
// timezone works the same way, an installation-wide answer and a personal one
// that overrides it.
type HolidayHandler struct {
	sessions *service.SessionService
	settings *service.SettingsService
	authz    *Authorizer
}

// NewHolidayHandler creates the handler.
func NewHolidayHandler(
	sessions *service.SessionService,
	settings *service.SettingsService,
	authz *Authorizer,
) *HolidayHandler {
	return &HolidayHandler{sessions: sessions, settings: settings, authz: authz}
}

// HolidayRegionRequest names a state by its code, or none.
type HolidayRegionRequest struct {
	Region string `json:"region"`
}

// InstallationHolidaysResponse is the installation's state, and what it could be.
type InstallationHolidaysResponse struct {
	Region  string   `json:"region"`
	Regions []string `json:"regions"`
}

// MyHolidaysResponse is whose holidays somebody keeps, and what they could choose.
type MyHolidaysResponse struct {
	// Region is their own choice: empty follows the installation, "none" keeps
	// none.
	Region string `json:"region"`

	// InstanceRegion is the installation's, for saying what following it means.
	InstanceRegion string `json:"instanceRegion"`

	// EffectiveRegion is what applies, empty for none.
	EffectiveRegion string `json:"effectiveRegion"`

	Regions []string `json:"regions"`
}

// Mine handles GET /api/v1/me/holidays.
func (h *HolidayHandler) Mine(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	instance, err := h.settings.HolidayRegion(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return MyHolidaysResponse{
		Region:          principal.User.HolidayRegion,
		InstanceRegion:  instance,
		EffectiveRegion: principal.User.HolidayRegionOf(instance),
		Regions:         model.HolidayRegions(),
	}, nil
}

// SetMine handles PUT /api/v1/me/holidays.
func (h *HolidayHandler) SetMine(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	var req HolidayRegionRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.sessions.SetHolidayRegion(c, principal.User.ID, req.Region); err != nil {
		return nil, toHTTPError(err)
	}

	return req, nil
}

// Installation handles GET /api/v1/settings/holidays.
func (h *HolidayHandler) Installation(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	region, err := h.settings.HolidayRegion(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return InstallationHolidaysResponse{Region: region, Regions: model.HolidayRegions()}, nil
}

// SetInstallation handles PUT /api/v1/settings/holidays.
func (h *HolidayHandler) SetInstallation(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireInstallationAdmin(c); err != nil {
		return nil, err
	}

	var req HolidayRegionRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.settings.SaveHolidayRegion(c, req.Region); err != nil {
		return nil, toHTTPError(err)
	}

	return req, nil
}
