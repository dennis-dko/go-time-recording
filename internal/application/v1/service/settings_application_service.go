package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/imaging"
	"github.com/dennis-dko/go-time-recording/internal/pkg/security"
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

	// secrets encrypts the one field in this table that is a credential rather
	// than a setting: the directory's bind password. The rest of the LDAP
	// configuration stays readable in the column, which is what makes a stored
	// setting worth reading when something is wrong with it.
	//
	// Never nil; a service built without one gets a sealer that stores what it is
	// given.
	secrets *security.Sealer
}

// WithSecrets attaches the key that encrypts the directory's bind password.
func (s *SettingsService) WithSecrets(secrets *security.Sealer) *SettingsService {
	if secrets != nil {
		s.secrets = secrets
	}

	return s
}

// NewSettingsService creates new instance.
func NewSettingsService(
	settings repository.SettingsRepository,
	roles repository.RoleRepository,
	appName string,
) *SettingsService {
	empty, _ := security.NewSealer("")

	return &SettingsService{settings: settings, roles: roles, appName: appName, secrets: empty}
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

	branding.TabTitle = all[model.SettingTabTitle]
	branding.Banner = all[model.SettingBannerText]
	branding.LogoDataURI = all[model.SettingLogoDataURI]
	branding.LogoHeader = all[model.SettingLogoHeader]
	branding.LogoBanner = all[model.SettingLogoBanner]
	branding.LogoIcon = all[model.SettingLogoIcon]
	branding.HeaderCrop = model.ParseLogoCrop(all[model.SettingLogoHeaderCrop])
	branding.BannerCrop = model.ParseLogoCrop(all[model.SettingLogoBannerCrop])
	branding.IconCrop = model.ParseLogoCrop(all[model.SettingLogoIconCrop])
	branding.FooterText = all[model.SettingFooterText]
	branding.CompanyName = all[model.SettingCompanyName]
	branding.CompanyURL = all[model.SettingCompanyURL]
	branding.LegalNotice = all[model.SettingFooterLegal]

	// What has been written in each language, where anything has. A language with
	// nothing written for it is left out rather than carried as four empty
	// strings, so "not translated" and "translated to nothing" stay different
	// answers.
	for _, language := range model.SupportedLanguages() {
		keys := model.BrandingKeysFor(language)

		text := model.BrandingText{
			Title:       all[keys.Title],
			TabTitle:    all[keys.TabTitle],
			Banner:      all[keys.Banner],
			FooterText:  all[keys.FooterText],
			LegalNotice: all[keys.LegalNotice],
		}

		if text == (model.BrandingText{}) {
			continue
		}

		if branding.Translations == nil {
			branding.Translations = map[string]model.BrandingText{}
		}

		branding.Translations[language] = text
	}

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

	// The lengths the form has always shown. All of them at once rather than the
	// first one over: this is one screen with one Save, and being told about a
	// long banner, fixing it, and then being told about a long footer is being
	// told half of what was wrong.
	if tooLong := overlongBranding(branding); len(tooLong) > 0 {
		return apperror.InvalidFields(tooLong...)
	}

	// Derived here, once, rather than every time a page is loaded. The three are
	// what every screen is actually given; the original is kept beside them so a
	// later change of size is a re-derivation rather than a request to every
	// installation to upload their logo again.
	//
	// A logo that cannot be converted is stored with no derivatives, and the
	// readers fall back to the original - which is also the state of every
	// installation whose logo predates this.
	header, banner, icon := deriveLogoSizes(branding)

	values := map[string]string{
		model.SettingAppTitle:    strings.TrimSpace(branding.Title),
		model.SettingTabTitle:    strings.TrimSpace(branding.TabTitle),
		model.SettingBannerText:  branding.Banner,
		model.SettingLogoDataURI: branding.LogoDataURI,
		model.SettingLogoHeader:  header,
		model.SettingLogoBanner:  banner,
		model.SettingLogoIcon:    icon,

		model.SettingLogoHeaderCrop: branding.HeaderCrop.String(),
		model.SettingLogoBannerCrop: branding.BannerCrop.String(),
		model.SettingLogoIconCrop:   branding.IconCrop.String(),
		model.SettingFooterText:     branding.FooterText,
		model.SettingCompanyName:    branding.CompanyName,
		model.SettingCompanyURL:     branding.CompanyURL,
		model.SettingFooterLegal:    branding.LegalNotice,
	}

	// The per-language texts, for every language the interface ships. Written
	// even when empty, because emptying one is how a translation is withdrawn -
	// skipping the blanks would make that impossible.
	for _, language := range model.SupportedLanguages() {
		keys := model.BrandingKeysFor(language)
		text := branding.Translations[language]

		values[keys.Title] = strings.TrimSpace(text.Title)
		values[keys.TabTitle] = strings.TrimSpace(text.TabTitle)
		values[keys.Banner] = text.Banner
		values[keys.FooterText] = text.FooterText
		values[keys.LegalNotice] = text.LegalNotice
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

// Telemetry returns the administered metrics and tracing settings, unresolved: a
// nil field means the configuration file's value still applies.
func (s *SettingsService) Telemetry(ctx context.Context) (model.Telemetry, error) {
	raw, err := s.settings.Get(ctx, model.SettingTelemetry)
	if err != nil {
		return model.Telemetry{}, err
	}

	var telemetry model.Telemetry

	if raw == "" {
		return telemetry, nil
	}

	if err := json.Unmarshal([]byte(raw), &telemetry); err != nil {
		// A corrupt entry falls back to the configuration file rather than
		// locking the administrator out of the screen that would repair it.
		return model.Telemetry{}, nil
	}

	return telemetry, nil
}

// SaveTelemetry stores the metrics and tracing settings for the next start.
func (s *SettingsService) SaveTelemetry(ctx context.Context, telemetry model.Telemetry) error {
	telemetry = telemetry.Normalise()

	if invalid := telemetry.InvalidTelemetryFields(); len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	raw, err := json.Marshal(telemetry)
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingTelemetry, string(raw))
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

	// A stored empty role beats the default, because unmarshalling writes the
	// field it finds over the one prepared above. Installations configured
	// before the role was checked on the way in have exactly that stored, and
	// what it produces is a picker with nothing selected - which saves the
	// emptiness again the next time the card is used. Repaired on the way out so
	// those installations come right without anybody knowing there was anything
	// to repair.
	if strings.TrimSpace(config.DefaultRole) == "" {
		config.DefaultRole = model.RoleUser
	}

	password, err := s.secrets.Open(config.BindPassword)
	if err != nil {
		// Same reasoning as the corrupt entry above, and a different consequence:
		// the screen opens, the password field shows as unset, and signing in
		// through the directory fails until somebody types it again. Refusing to
		// answer at all would hide the one setting that explains why.
		return config, nil
	}

	config.BindPassword = password

	return config, nil
}

// SaveLDAP stores the directory configuration.
func (s *SettingsService) SaveLDAP(ctx context.Context, config model.LDAPConfig) error {
	config.SyncSchedule = strings.TrimSpace(config.SyncSchedule)

	// Checked whether or not the directory is enabled, because the schedule is
	// stored either way and the scheduler's own answer to an expression it cannot
	// read is a line in the log and no job at all. An administrator would be told
	// their nightly reconciliation was saved and it would simply never run.
	if !model.ValidSchedule(config.SyncSchedule) {
		return apperror.InvalidFields("syncSchedule")
	}

	// Whether or not the directory is switched on, for the same reason the
	// schedule above is: the card is saved repeatedly while it is being
	// configured and switched on last, so everything checked only when enabled is
	// checked after it has already been stored wrong.
	config.DefaultRole = strings.TrimSpace(config.DefaultRole)
	if config.DefaultRole == "" {
		config.DefaultRole = model.RoleUser
	}

	// An unknown default role would leave provisioned accounts unusable.
	if _, err := s.roles.GetByName(ctx, config.DefaultRole); err != nil {
		return apperror.InvalidFields("defaultRole")
	}

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
	}

	// Sealed after everything above has been checked, and only this field: the
	// rest of the configuration is a setting rather than a credential, and a
	// stored setting is worth being able to read when something is wrong with it.
	sealed, err := s.secrets.Seal(config.BindPassword)
	if err != nil {
		return apperror.Internal(err)
	}

	config.BindPassword = sealed

	raw, err := json.Marshal(config)
	if err != nil {
		return apperror.Internal(err)
	}

	return s.settings.Set(ctx, model.SettingLDAPSettings, string(raw))
}

// overlongBranding names the labelling fields that are past their limit.
//
// By the name the payload uses, because that is what the interface looks each one
// up under to say it the way the label above the box does.
// Written out rather than looped over a table of name and limit, so each name is
// a literal where it is used: the test that checks every rejectable field has a
// label reads them out of this source, and a name assembled at runtime is a name
// it cannot see.
func overlongBranding(branding model.Branding) []string {
	invalid := make([]string, 0)

	if model.TooLong(branding.Title, model.MaxTitleLength) {
		invalid = append(invalid, "title")
	}

	if model.TooLong(branding.TabTitle, model.MaxTabTitleLength) {
		invalid = append(invalid, "tabTitle")
	}

	if model.TooLong(branding.Banner, model.MaxBannerLength) {
		invalid = append(invalid, "banner")
	}

	if model.TooLong(branding.FooterText, model.MaxFooterTextLength) {
		invalid = append(invalid, "footerText")
	}

	if model.TooLong(branding.LegalNotice, model.MaxLegalNoticeLength) {
		invalid = append(invalid, "legalNotice")
	}

	// The same ceilings for a translation, since it lands in the same places. The
	// field is named without the language: the form shows one language at a time,
	// so "the banner is too long" is about the one on screen.
	for _, text := range branding.Translations {
		if model.TooLong(text.Title, model.MaxTitleLength) {
			invalid = append(invalid, "title")
		}

		if model.TooLong(text.TabTitle, model.MaxTabTitleLength) {
			invalid = append(invalid, "tabTitle")
		}

		if model.TooLong(text.Banner, model.MaxBannerLength) {
			invalid = append(invalid, "banner")
		}

		if model.TooLong(text.FooterText, model.MaxFooterTextLength) {
			invalid = append(invalid, "footerText")
		}

		if model.TooLong(text.LegalNotice, model.MaxLegalNoticeLength) {
			invalid = append(invalid, "legalNotice")
		}
	}

	if model.TooLong(branding.CompanyName, model.MaxCompanyNameLength) {
		invalid = append(invalid, "companyName")
	}

	return invalid
}

// validateLogo checks the inline image.
func validateLogo(dataURI string) error {
	if dataURI == "" {
		return nil
	}

	if !strings.HasPrefix(dataURI, "data:image/") {
		return apperror.Invalidf("the logo must be an inline image (data:image/...)").
			WithCode("logoNotInline")
	}

	// PNG or JPEG, and nothing else.
	//
	// This used to take any image the browser would encode, SVG included, and SVG
	// is where it went wrong: the same file that renders perfectly in the header
	// and on the sign-in screen can be refused as a tab icon, silently, by an
	// engine that has its own rules about what it will rasterise. Nothing in the
	// response says so - the icon is served, fetched, and then not used.
	//
	// A raster image has no such argument with anybody. It costs an installation
	// one export from whatever drew the logo, and it costs this application the
	// class of failure where a logo is configured, correct, and invisible in one
	// browser.
	if !isRasterImage(dataURI) {
		return apperror.Invalidf("the logo must be a PNG or a JPEG").
			WithCode("logoNotRaster")
	}

	if len(dataURI) > maxLogoBytes {
		return apperror.Invalidf("the logo must be smaller than %d KB", maxLogoBytes/1024).
			WithCode("logoTooLarge", maxLogoBytes/1024)
	}

	// And how large it is once decoded, which the byte count does not say. A PNG
	// of one flat colour compresses at about 1250:1, so 8000 by 8000 arrives as
	// 199 KB - inside the limit above - and becomes 244 MB in memory, three times
	// over, once per derivative.
	//
	// Refused here rather than only inside imaging, because imaging is reached
	// from deriveLogoSizes, which swallows its errors on purpose: a logo that
	// scales badly must not refuse the title beside it. That is right for a logo
	// that is merely awkward and wrong for one that cannot be used at all - it
	// would be stored, served to every visitor at full size, and silently missing
	// from the header, the sign-in card and the tab, with nothing said.
	pixels, err := imaging.PixelsIn(dataURI)
	if err != nil {
		return apperror.Invalidf("the logo must be a PNG or a JPEG").
			WithCode("logoNotRaster")
	}

	if pixels > imaging.MaxPixels {
		return apperror.Invalidf("the logo must be at most %d megapixels",
			imaging.MaxPixels>>20).
			WithCode("logoTooManyPixels", imaging.MaxPixels>>20)
	}

	return nil
}

// isRasterImage reports whether the data URI holds one of the two formats every
// browser will take as a tab icon.
//
// Read from the URI's own declaration rather than by sniffing the bytes: the
// browser wrote it from the file it was given, and the same string is what the
// icon endpoint later serves as a content type. Sniffing would let those two
// disagree, which is the one thing that must not happen for an image served under
// a type somebody else chose.
func isRasterImage(dataURI string) bool {
	head, _, found := strings.Cut(dataURI, ",")
	if !found {
		return false
	}

	kind := strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")

	return kind == "image/png" || kind == "image/jpeg"
}

func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// deriveLogoSizes makes the three shown versions of a logo.
//
// Failures are silent on purpose: this runs inside saving the appearance, and a
// logo that scales badly is not a reason to refuse the title beside it. What
// happens instead is that the derivatives are empty and every reader falls back
// to the original, which is exactly what an installation whose logo predates this
// already does.
func deriveLogoSizes(branding model.Branding) (header, banner, icon string) {
	logo := branding.LogoDataURI

	if strings.TrimSpace(logo) == "" {
		return "", "", ""
	}

	fit := func(crop model.LogoCrop, width, height int) string {
		out, err := imaging.Fit(logo, asCrop(crop), width, height)
		if err != nil {
			return ""
		}

		return out
	}

	// The tab's is padded out to the square, which the other two must not be -
	// see FitIcon.
	tab, err := imaging.FitIcon(logo, asCrop(branding.IconCrop))
	if err != nil {
		tab = ""
	}

	return fit(branding.HeaderCrop, imaging.HeaderWidth, imaging.HeaderHeight),
		fit(branding.BannerCrop, imaging.BannerWidth, imaging.BannerHeight),
		tab
}

// asCrop carries a crop across the layer boundary.
//
// Two identical little structs rather than one shared: the domain names what an
// installation chose, and the imaging package names what a scaler is given. They
// agree today and there is no reason for either to know the other's type.
func asCrop(c model.LogoCrop) imaging.Crop {
	return imaging.Crop{X: c.X, Y: c.Y, W: c.W, H: c.H}
}
