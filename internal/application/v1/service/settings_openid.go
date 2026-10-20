package service

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// What a provider's configuration may hold. The issuer is bounded by the column
// a linked identity keeps it in, the client id by what providers hand out, and
// the label by the button it is written on.
const (
	maxOpenIDIssuerLength   = 255
	maxOpenIDClientIDLength = 255
	maxOpenIDLabelLength    = 60
)

// OpenID returns the provider people sign in through, as stored, with its client
// secret opened.
func (s *SettingsService) OpenID(ctx context.Context) (model.OpenIDConfig, error) {
	raw, err := s.settings.Get(ctx, model.SettingOpenID)
	if err != nil {
		return model.OpenIDConfig{}, err
	}

	var config model.OpenIDConfig

	if raw == "" {
		return config, nil
	}

	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		// As with the directory: a corrupt entry must not lock the administrator out
		// of the screen that would let them repair it.
		return model.OpenIDConfig{}, nil
	}

	secret, err := s.secrets.Open(config.ClientSecret)
	if err != nil {
		// The screen opens with the secret shown as unset, and signing in through
		// the provider fails until somebody types it again - which the screen can
		// say, where refusing to answer would hide the one setting that explains it.
		config.ClientSecret = ""

		return config, nil
	}

	config.ClientSecret = secret

	return config, nil
}

// SaveOpenID stores the provider people sign in through.
//
// Checked whether or not it is switched on, as the directory's settings are: the
// card is saved while it is being filled in and switched on last, so a check made
// only when enabled is made after the wrong value has been stored.
func (s *SettingsService) SaveOpenID(ctx context.Context, config model.OpenIDConfig) error {
	config.Issuer = strings.TrimSpace(config.Issuer)
	config.ClientID = strings.TrimSpace(config.ClientID)
	config.Label = strings.TrimSpace(config.Label)

	var invalid []string

	if model.TooLong(config.Label, maxOpenIDLabelLength) {
		invalid = append(invalid, "label")
	}

	if model.TooLong(config.ClientID, maxOpenIDClientIDLength) || (config.Enabled && config.ClientID == "") {
		invalid = append(invalid, "clientId")
	}

	if config.Issuer != "" || config.Enabled {
		issuer, err := url.Parse(config.Issuer)
		if err != nil || issuer.Host == "" || (issuer.Scheme != "https" && issuer.Scheme != "http") ||
			model.TooLong(config.Issuer, maxOpenIDIssuerLength) {
			invalid = append(invalid, "issuer")
		} else if issuer.Scheme == "http" && !loopbackHost(issuer.Hostname()) {
			// Over plain HTTP anybody on the way could answer for the provider - its
			// discovery document and its keys - and so sign anybody in as anybody. A
			// provider on this machine is the one place that cannot happen.
			return apperror.Invalidf("a sign-in provider has to be reached over HTTPS").
				WithCode("openidNeedsHttps")
		}
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	// Sealed after everything above has been checked, and only this field, for the
	// reason the directory's bind password is.
	sealed, err := s.secrets.Seal(config.ClientSecret)
	if err != nil {
		return apperror.Internal(err)
	}

	config.ClientSecret = sealed

	raw, err := json.Marshal(config)
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingOpenID, string(raw))
}

// loopbackHost reports whether a host names this machine.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
