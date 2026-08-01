package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// maxLogoBytes caps the inline logo. It is stored as a data URI in the
// database and sent with every page, so a large image would slow every load.
const maxLogoBytes = 256 * 1024

// SettingsService administers branding and the LDAP connection.
type SettingsService struct {
	settings repository.SettingsRepository
	roles    repository.RoleRepository
}

// NewSettingsService creates new instance.
func NewSettingsService(
	settings repository.SettingsRepository,
	roles repository.RoleRepository,
) *SettingsService {
	return &SettingsService{settings: settings, roles: roles}
}

// Branding returns the instance labelling, with defaults filled in.
func (s *SettingsService) Branding(ctx context.Context) (model.Branding, error) {
	all, err := s.settings.GetAll(ctx)
	if err != nil {
		return model.Branding{}, err
	}

	branding := model.DefaultBranding()

	if v := all[model.SettingAppTitle]; v != "" {
		branding.Title = v
	}

	branding.Banner = all[model.SettingBannerText]
	branding.LogoDataURI = all[model.SettingLogoDataURI]
	branding.FooterText = all[model.SettingFooterText]
	branding.CompanyName = all[model.SettingCompanyName]
	branding.CompanyURL = all[model.SettingCompanyURL]
	branding.LegalNotice = all[model.SettingFooterLegal]

	return branding, nil
}

// SaveBranding stores the instance labelling.
func (s *SettingsService) SaveBranding(ctx context.Context, branding model.Branding) error {
	if err := validateLogo(branding.LogoDataURI); err != nil {
		return err
	}

	if branding.CompanyURL != "" && !isHTTPURL(branding.CompanyURL) {
		return apperror.InvalidFields("companyUrl")
	}

	values := map[string]string{
		model.SettingAppTitle:    strings.TrimSpace(branding.Title),
		model.SettingBannerText:  branding.Banner,
		model.SettingLogoDataURI: branding.LogoDataURI,
		model.SettingFooterText:  branding.FooterText,
		model.SettingCompanyName: branding.CompanyName,
		model.SettingCompanyURL:  branding.CompanyURL,
		model.SettingFooterLegal: branding.LegalNotice,
	}

	for key, value := range values {
		if err := s.settings.Set(ctx, key, value); err != nil {
			return err
		}
	}

	return nil
}

// LDAP returns the stored directory configuration, or the defaults.
func (s *SettingsService) LDAP(ctx context.Context) (model.LDAPConfig, error) {
	raw, err := s.settings.Get(ctx, model.SettingLDAPSettings)
	if err != nil {
		return model.LDAPConfig{}, err
	}

	config := model.DefaultLDAPConfig()

	if raw == "" {
		return config, nil
	}

	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		// A corrupt entry must not lock the administrator out of the screen
		// that would let them repair it.
		return model.DefaultLDAPConfig(), nil
	}

	return config, nil
}

// SaveLDAP stores the directory configuration.
func (s *SettingsService) SaveLDAP(ctx context.Context, config model.LDAPConfig) error {
	if config.Enabled {
		var invalid []string

		if strings.TrimSpace(config.Host) == "" {
			invalid = append(invalid, "host")
		}

		if strings.TrimSpace(config.BaseDN) == "" {
			invalid = append(invalid, "baseDn")
		}

		if !strings.Contains(config.UserFilter, "%s") {
			invalid = append(invalid, "userFilter")
		}

		if config.Port <= 0 || config.Port > 65535 {
			invalid = append(invalid, "port")
		}

		if len(invalid) > 0 {
			return apperror.InvalidFields(invalid...)
		}

		// An unknown default role would leave provisioned accounts unusable.
		if _, err := s.roles.GetByName(ctx, config.DefaultRole); err != nil {
			return apperror.InvalidFields("defaultRole")
		}
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingLDAPSettings, string(raw))
}

// validateLogo checks the inline image.
func validateLogo(dataURI string) error {
	if dataURI == "" {
		return nil
	}

	if !strings.HasPrefix(dataURI, "data:image/") {
		return apperror.Invalidf("the logo must be an inline image (data:image/...)")
	}

	if len(dataURI) > maxLogoBytes {
		return apperror.Invalidf("the logo must be smaller than %d KB", maxLogoBytes/1024)
	}

	return nil
}

func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}
