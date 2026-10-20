package rest

import (
	"context"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// OpenIDSettingsRequest mirrors model.OpenIDConfig on the wire. The client secret
// is write-only: it is accepted but never sent back, and sending none keeps the
// stored one.
type OpenIDSettingsRequest struct {
	Enabled      bool   `json:"enabled"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Label        string `json:"label"`
}

// OpenIDSettingsResponse is the stored provider without its secret.
type OpenIDSettingsResponse struct {
	Enabled  bool   `json:"enabled"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	Label    string `json:"label"`

	// HasSecret tells the screen a secret is stored without revealing it.
	HasSecret bool `json:"hasSecret"`

	// RedirectURI is the address the provider has to be told to send people back
	// to: this installation's own, as the browser that asked reached it.
	RedirectURI string `json:"redirectUri"`
}

// WithOpenIDCheck lets the Settings screen ask whether a provider can be found
// before anybody's sign-in depends on it.
func (h *SettingsHandler) WithOpenIDCheck(check func(ctx context.Context, issuer string) error) *SettingsHandler {
	h.openidCheck = check

	return h
}

// OpenID handles GET /api/v1/settings/openid.
func (h *SettingsHandler) OpenID(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	config, err := h.settings.OpenID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return h.newOpenIDSettingsResponse(c, config), nil
}

// SaveOpenID handles PUT /api/v1/settings/openid. It applies at once: the next
// sign-in reads the provider as stored now.
func (h *SettingsHandler) SaveOpenID(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req OpenIDSettingsRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	config := model.OpenIDConfig{
		Enabled: req.Enabled, Issuer: req.Issuer, ClientID: req.ClientID,
		ClientSecret: req.ClientSecret, Label: req.Label,
	}

	if config.ClientSecret == "" {
		stored, err := h.settings.OpenID(c)
		if err != nil {
			return nil, toHTTPError(err)
		}

		config.ClientSecret = stored.ClientSecret
	}

	if err := h.settings.SaveOpenID(c, config); err != nil {
		return nil, toHTTPError(err)
	}

	saved, err := h.settings.OpenID(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return h.newOpenIDSettingsResponse(c, saved), nil
}

// TestOpenID handles POST /api/v1/settings/openid/test, which reads the
// provider's discovery document.
//
// A failed test is information rather than a fault, so it answers as the
// directory's test does - with what the provider said underneath, because why a
// discovery document cannot be read is not a fixed set of sentences.
func (h *SettingsHandler) TestOpenID(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req OpenIDSettingsRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if h.openidCheck == nil {
		return nil, toHTTPError(apperror.Internal(nil))
	}

	if err := h.openidCheck(c, req.Issuer); err != nil {
		failure := probeFailure(err)

		if detail, ok := apperror.Detail(err); ok && detail.Err != nil {
			failure["detail"] = detail.Err.Error()
		}

		return map[string]any{"ok": false, "error": failure}, nil
	}

	return map[string]any{"ok": true}, nil
}

func (h *SettingsHandler) newOpenIDSettingsResponse(c *gofr.Context, config model.OpenIDConfig) OpenIDSettingsResponse {
	return OpenIDSettingsResponse{
		Enabled: config.Enabled, Issuer: config.Issuer, ClientID: config.ClientID, Label: config.Label,
		HasSecret:   config.ClientSecret != "",
		RedirectURI: openIDRedirectURI(c),
	}
}
