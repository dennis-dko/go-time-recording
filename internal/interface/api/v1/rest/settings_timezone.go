package rest

import (
	"gofr.dev/pkg/gofr"
)

// TimezoneRequest carries the instance-wide zone.
type TimezoneRequest struct {
	Timezone string `json:"timezone"`
}

// Timezone handles GET /api/v1/settings/timezone.
func (h *SettingsHandler) Timezone(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	timezone, err := h.settings.Timezone(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return TimezoneRequest{Timezone: timezone}, nil
}

// SaveTimezone handles PUT /api/v1/settings/timezone.
func (h *SettingsHandler) SaveTimezone(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req TimezoneRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.settings.SaveTimezone(c, req.Timezone); err != nil {
		return nil, toHTTPError(err)
	}

	return TimezoneRequest{Timezone: req.Timezone}, nil
}
