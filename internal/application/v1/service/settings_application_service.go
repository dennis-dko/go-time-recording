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

	// appName is APP_NAME from the environment, used as the instance title
	// until an administrator sets one under Settings.
	appName string
}

// NewSettingsService creates new instance.
func NewSettingsService(
	settings repository.SettingsRepository,
	roles repository.RoleRepository,
	appName string,
) *SettingsService {
	return &SettingsService{settings: settings, roles: roles, appName: appName}
}

// Branding returns the instance labelling, with defaults filled in.
func (s *SettingsService) Branding(ctx context.Context) (model.Branding, error) {
	all, err := s.settings.GetAll(ctx)
	if err != nil {
		return model.Branding{}, err
	}

	branding := model.DefaultBranding(s.appName)

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

// Raw returns a stored setting exactly as it is, with no default filled in.
//
// The setup wizard needs this rather than the resolved accessors: those cannot
// tell "the administrator chose UTC" from "nobody has chosen anything", and
// only the second is a step still to do.
func (s *SettingsService) Raw(ctx context.Context, key string) (string, error) {
	return s.settings.Get(ctx, key)
}

// SetupCompleted reports whether the first-run wizard was dismissed.
func (s *SettingsService) SetupCompleted(ctx context.Context) (bool, error) {
	stored, err := s.settings.Get(ctx, model.SettingSetupCompleted)
	if err != nil {
		return false, err
	}

	return stored == "true", nil
}

// SetSetupCompleted records the wizard as dismissed, or brings it back.
func (s *SettingsService) SetSetupCompleted(ctx context.Context, completed bool) error {
	value := "false"
	if completed {
		value = "true"
	}

	return s.settings.Set(ctx, model.SettingSetupCompleted, value)
}

// Operational returns the administered limits, unresolved: a nil field means
// the environment's value still applies.
func (s *SettingsService) Operational(ctx context.Context) (model.Operational, error) {
	raw, err := s.settings.Get(ctx, model.SettingOperational)
	if err != nil {
		return model.Operational{}, err
	}

	var operational model.Operational

	if raw == "" {
		return operational, nil
	}

	if err := json.Unmarshal([]byte(raw), &operational); err != nil {
		// A corrupt entry falls back to the environment rather than locking the
		// administrator out of the screen that would let them repair it.
		return model.Operational{}, nil
	}

	return operational, nil
}

// Maintenance reports whether the installation is out of service.
//
// A corrupt or unreadable entry reads as off. The alternative - failing closed -
// would turn a bad byte in one settings row into an outage nobody can end,
// because ending it needs the screen that this same call feeds.
func (s *SettingsService) Maintenance(ctx context.Context) (model.Maintenance, error) {
	raw, err := s.settings.Get(ctx, model.SettingMaintenance)
	if err != nil {
		return model.Maintenance{}, err
	}

	var maintenance model.Maintenance

	if raw == "" {
		return maintenance, nil
	}

	if err := json.Unmarshal([]byte(raw), &maintenance); err != nil {
		return model.Maintenance{}, nil
	}

	return maintenance, nil
}

// SaveMaintenance turns maintenance mode on or off.
func (s *SettingsService) SaveMaintenance(ctx context.Context, maintenance model.Maintenance) error {
	encoded, err := json.Marshal(maintenance.Normalise())
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingMaintenance, string(encoded))
}

// SaveOperational stores the administered limits.
func (s *SettingsService) SaveOperational(ctx context.Context, operational model.Operational) error {
	if invalid := operational.InvalidOperationalFields(); len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	raw, err := json.Marshal(operational)
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingOperational, string(raw))
}

// Timezone returns the instance-wide zone, or the default.
//
// It falls back rather than erroring on an unknown name, because this is read
// on the way to rendering nearly every page: a zone that stopped being valid
// should shift the display, not take the application down.
func (s *SettingsService) Timezone(ctx context.Context) (string, error) {
	stored, err := s.settings.Get(ctx, model.SettingTimezone)
	if err != nil {
		return model.DefaultTimezone, err
	}

	if !model.IsSupportedTimezone(stored) {
		return model.DefaultTimezone, nil
	}

	return stored, nil
}

// SaveTimezone stores the instance-wide zone.
func (s *SettingsService) SaveTimezone(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)

	// Rejected rather than quietly corrected: a name that does not resolve
	// would move everyone's bookings to a different day than the administrator
	// intended, and they would have no way to tell from the screen.
	if !model.IsSupportedTimezone(name) {
		return apperror.InvalidFields("timezone")
	}

	return s.settings.Set(ctx, model.SettingTimezone, name)
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
