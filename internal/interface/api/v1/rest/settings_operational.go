package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// OperationalResponse carries the administered limits together with what the
// environment configured, so the screen can show what a blank field means
// instead of leaving the reader to guess.
type OperationalResponse struct {
	// Configured holds only what has been overridden; an absent field follows
	// the environment.
	Configured model.Operational `json:"configured"`

	// Effective is what is actually in force right now.
	Effective OperationalLimits `json:"effective"`

	// Defaults is what the environment supplies, shown as the placeholder in
	// each empty field.
	Defaults OperationalLimits `json:"defaults"`
}

// OperationalLimits is model.Limits on the wire.
//
// Every field the form has a box for belongs here, and one did not: the idle
// timeout was in model.Limits and in the markup and missing from this, which is
// what the screen reads for both halves of what it tells the reader. So the
// placeholder that says what leaving a field empty will do was empty on that one
// field, and the line naming what is in force named the other five.
type OperationalLimits struct {
	SessionLifetimeHours   float64 `json:"sessionLifetimeHours"`
	SessionIdleMinutes     float64 `json:"sessionIdleMinutes"`
	MaxDailyHours          float64 `json:"maxDailyHours"`
	RateLimit              int     `json:"rateLimit"`
	RateLimitWindowSeconds int     `json:"rateLimitWindowSeconds"`
	LDAPSyncMaxDeleteRatio float64 `json:"ldapSyncMaxDeleteRatio"`
}

func newOperationalLimits(l model.Limits) OperationalLimits {
	return OperationalLimits{
		SessionLifetimeHours:   l.SessionLifetimeHours,
		SessionIdleMinutes:     l.SessionIdleMinutes,
		MaxDailyHours:          l.MaxDailyHours,
		RateLimit:              l.RateLimit,
		RateLimitWindowSeconds: l.RateLimitWindowSeconds,
		LDAPSyncMaxDeleteRatio: l.LDAPSyncMaxDeleteRatio,
	}
}

// Operational handles GET /api/v1/settings/operational.
func (h *SettingsHandler) Operational(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	configured, err := h.settings.Operational(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return OperationalResponse{
		Configured: configured,
		Effective:  newOperationalLimits(h.limits.Limits(c)),
		Defaults:   newOperationalLimits(h.limits.Fallback()),
	}, nil
}

// SaveOperational handles PUT /api/v1/settings/operational.
func (h *SettingsHandler) SaveOperational(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req model.Operational
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.settings.SaveOperational(c, req); err != nil {
		return nil, toHTTPError(err)
	}

	// Without this the caller would read back the values they just replaced,
	// for as long as the cache holds them.
	h.limits.Invalidate()

	return OperationalResponse{
		Configured: req,
		Effective:  newOperationalLimits(h.limits.Limits(c)),
		Defaults:   newOperationalLimits(h.limits.Fallback()),
	}, nil
}
