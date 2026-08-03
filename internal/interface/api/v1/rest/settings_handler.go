package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// SettingsHandler serves the administration screen: branding, the database
// connection and the directory.
type SettingsHandler struct {
	settings *service.SettingsService
	authz    *Authorizer
	ldap     *ldapAdmin

	// limits is dropped from cache after a save, so an administrator sees the
	// change take effect immediately rather than after the refresh interval.
	limits *service.LimitsProvider

	// activeDialect is what this process actually connected to, which differs
	// from the stored settings until the next restart.
	activeDialect string

	// version is the build this process was compiled from, reported alongside
	// the branding because the footer renders both and one request is better
	// than two for something on every page.
	version string
}

// ldapAdmin is the subset of the LDAP client this handler drives, kept as an
// interface so the REST layer does not depend on the client package.
type ldapAdmin struct {
	configure func(model.LDAPConfig)
	test      func(*gofr.Context, model.LDAPConfig) error
}

// NewSettingsHandler creates the handler.
func NewSettingsHandler(
	settings *service.SettingsService,
	authz *Authorizer,
	limits *service.LimitsProvider,
	activeDialect string,
	version string,
	configure func(model.LDAPConfig),
	test func(*gofr.Context, model.LDAPConfig) error,
) *SettingsHandler {
	return &SettingsHandler{
		settings:      settings,
		authz:         authz,
		limits:        limits,
		activeDialect: activeDialect,
		version:       version,
		ldap:          &ldapAdmin{configure: configure, test: test},
	}
}

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
type OperationalLimits struct {
	SessionLifetimeHours   float64 `json:"sessionLifetimeHours"`
	MaxDailyHours          float64 `json:"maxDailyHours"`
	RateLimit              int     `json:"rateLimit"`
	RateLimitWindowSeconds int     `json:"rateLimitWindowSeconds"`
	AutoCloseAfterDays     int     `json:"autoCloseAfterDays"`
	LDAPSyncMaxDeleteRatio float64 `json:"ldapSyncMaxDeleteRatio"`
}

func newOperationalLimits(l model.Limits) OperationalLimits {
	return OperationalLimits{
		SessionLifetimeHours:   l.SessionLifetimeHours,
		MaxDailyHours:          l.MaxDailyHours,
		RateLimit:              l.RateLimit,
		RateLimitWindowSeconds: l.RateLimitWindowSeconds,
		AutoCloseAfterDays:     l.AutoCloseAfterDays,
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

// BrandingResponse is the instance labelling.
type BrandingResponse struct {
	Title       string `json:"title"`
	Banner      string `json:"banner"`
	Logo        string `json:"logo"`
	FooterText  string `json:"footerText"`
	CompanyName string `json:"companyName"`
	CompanyURL  string `json:"companyUrl"`
	LegalNotice string `json:"legalNotice"`
}

// InstanceResponse is the branding plus the build serving it.
//
// Embedded rather than nested, so the JSON stays the flat object the interface
// already reads. It is the GET shape only: SaveBranding still binds
// BrandingResponse, which is what keeps a PUT from carrying a version field
// that nothing could act on.
type InstanceResponse struct {
	BrandingResponse

	// Version is the build this process was compiled from - a tag for a
	// release, "dev" for a binary built without -ldflags. Public, like the rest
	// of the branding: it is in the footer of a page anyone can reach, and a
	// version number is not what keeps an installation safe.
	Version string `json:"version"`
}

// Branding handles GET /api/v1/branding.
//
// It is readable by anyone, signed in or not: the sign-in screen has to show
// the instance's own title and logo before there is a session.
func (h *SettingsHandler) Branding(c *gofr.Context) (any, error) {
	branding, err := h.settings.Branding(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return InstanceResponse{
		BrandingResponse: newBrandingResponse(branding),
		Version:          h.version,
	}, nil
}

// SaveBranding handles PUT /api/v1/settings/branding.
func (h *SettingsHandler) SaveBranding(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req BrandingResponse
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	err := h.settings.SaveBranding(c, model.Branding{
		Title:       req.Title,
		Banner:      req.Banner,
		LogoDataURI: req.Logo,
		FooterText:  req.FooterText,
		CompanyName: req.CompanyName,
		CompanyURL:  req.CompanyURL,
		LegalNotice: req.LegalNotice,
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	branding, err := h.settings.Branding(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// The same shape GET returns, so the interface can render the saved result
	// with the code that renders a fetched one.
	return InstanceResponse{
		BrandingResponse: newBrandingResponse(branding),
		Version:          h.version,
	}, nil
}

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

// LDAPRequest mirrors model.LDAPConfig on the wire. The bind password is
// write-only: it is accepted but never sent back.
type LDAPRequest struct {
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	StartTLS       bool   `json:"startTls"`
	UseTLS         bool   `json:"useTls"`
	SkipVerify     bool   `json:"skipVerify"`
	BindDN         string `json:"bindDn"`
	BindPassword   string `json:"bindPassword"`
	BaseDN         string `json:"baseDn"`
	UserFilter     string `json:"userFilter"`
	NameAttribute  string `json:"nameAttribute"`
	EmailAttribute string `json:"emailAttribute"`

	// IDAttribute is the identifier that survives a rename. Without it the
	// synchronisation matches on the mail address, and a renamed mailbox reads
	// as a departure that takes the person's recorded hours with it.
	IDAttribute string `json:"idAttribute"`

	DefaultRole string `json:"defaultRole"`
}

// LDAPResponse is the stored configuration without the password.
type LDAPResponse struct {
	LDAPRequest

	// HasPassword tells the UI a password is stored without revealing it.
	HasPassword bool `json:"hasPassword"`
}

// LDAP handles GET /api/v1/settings/ldap.
func (h *SettingsHandler) LDAP(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	config, err := h.settings.LDAP(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newLDAPResponse(config), nil
}

// SaveLDAP handles PUT /api/v1/settings/ldap.
func (h *SettingsHandler) SaveLDAP(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	config, err := h.bindLDAP(c)
	if err != nil {
		return nil, err
	}

	if err := h.settings.SaveLDAP(c, config); err != nil {
		return nil, toHTTPError(err)
	}

	// Applied immediately: the next sign-in uses the new directory.
	if h.ldap.configure != nil {
		h.ldap.configure(config)
	}

	return newLDAPResponse(config), nil
}

// TestLDAP handles POST /api/v1/settings/ldap/test.
func (h *SettingsHandler) TestLDAP(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	config, err := h.bindLDAP(c)
	if err != nil {
		return nil, err
	}

	if h.ldap.test == nil {
		return nil, toHTTPError(apperror.Internal(nil))
	}

	if err := h.ldap.test(c, config); err != nil {
		// A failed test is information, not a server fault, so it comes back
		// as a readable message rather than a 500.
		return map[string]any{"ok": false, "message": err.Error()}, nil
	}

	return map[string]any{"ok": true, "message": "connection established"}, nil
}

// bindLDAP reads the payload, keeping the stored password when the client
// sends none back.
func (h *SettingsHandler) bindLDAP(c *gofr.Context) (model.LDAPConfig, error) {
	var req LDAPRequest
	if err := bind(c, &req); err != nil {
		return model.LDAPConfig{}, toHTTPError(err)
	}

	config := model.LDAPConfig{
		Enabled:        req.Enabled,
		Host:           req.Host,
		Port:           req.Port,
		StartTLS:       req.StartTLS,
		UseTLS:         req.UseTLS,
		SkipVerify:     req.SkipVerify,
		BindDN:         req.BindDN,
		BindPassword:   req.BindPassword,
		BaseDN:         req.BaseDN,
		UserFilter:     req.UserFilter,
		NameAttribute:  req.NameAttribute,
		EmailAttribute: req.EmailAttribute,
		IDAttribute:    req.IDAttribute,
		DefaultRole:    req.DefaultRole,
	}

	if config.BindPassword == "" {
		stored, err := h.settings.LDAP(c)
		if err != nil {
			return model.LDAPConfig{}, toHTTPError(err)
		}

		config.BindPassword = stored.BindPassword
	}

	return config, nil
}

// DatasourceRequest is the administered database connection.
type DatasourceRequest struct {
	Dialect  string `json:"dialect"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslMode"`
}

// DatasourceResponse reports the stored connection without its password.
type DatasourceResponse struct {
	DatasourceRequest

	HasPassword bool `json:"hasPassword"`

	// Active is what the running process is actually connected to, which
	// differs from the stored settings until the next restart.
	Active string `json:"active"`

	// RestartRequired is always true after a change: GoFr opens the database
	// at start-up, and swapping it under running requests is not safe.
	RestartRequired bool `json:"restartRequired"`
}

// Datasource handles GET /api/v1/settings/datasource.
func (h *SettingsHandler) Datasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile)

	resp := DatasourceResponse{Active: h.activeDialect}
	if ok {
		resp.DatasourceRequest = DatasourceRequest{
			Dialect: stored.Dialect,
			Name:    stored.Name,
			Host:    stored.Host,
			Port:    stored.Port,
			User:    stored.User,
			SSLMode: stored.SSLMode,
		}
		resp.HasPassword = stored.Password != ""
	}

	return resp, nil
}

// SaveDatasource handles PUT /api/v1/settings/datasource.
//
// The connection is written to a file and takes effect on the next start; it
// is deliberately not swapped at run time.
func (h *SettingsHandler) SaveDatasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req DatasourceRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	ds := appconfig.Datasource{
		Dialect:  req.Dialect,
		Name:     req.Name,
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		SSLMode:  req.SSLMode,
	}

	// Keep the stored password when the client sends none back.
	if ds.Password == "" {
		if stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok {
			ds.Password = stored.Password
		}
	}

	if err := ds.Validate(); err != nil {
		return nil, toHTTPError(apperror.Invalidf("%v", err))
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, ds); err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	return map[string]any{
		"status":          "saved",
		"restartRequired": true,
		"message":         "The connection is applied when the application is restarted.",
	}, nil
}

// TestDatasource handles POST /api/v1/settings/datasource/test.
//
// It probes the connection without saving or switching to it, so the
// administrator can find a typo before restarting into a broken setting.
func (h *SettingsHandler) TestDatasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req DatasourceRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	ds := appconfig.Datasource{
		Dialect:  req.Dialect,
		Name:     req.Name,
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		SSLMode:  req.SSLMode,
	}

	// An empty password means "use the stored one", the same as on save.
	if ds.Password == "" {
		if stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok {
			ds.Password = stored.Password
		}
	}

	if err := appconfig.TestDatasource(c, ds); err != nil {
		// A failed probe is information, not a server fault.
		return map[string]any{"ok": false, "message": err.Error()}, nil
	}

	return map[string]any{"ok": true, "message": "connection established"}, nil
}

// requireAdmin restricts the screen to the built-in administrator: these
// settings decide where the data lives and who may sign in at all.
func (h *SettingsHandler) requireAdmin(c *gofr.Context) error {
	_, err := h.authz.RequireSystemAdmin(c)

	return err
}

func newBrandingResponse(b model.Branding) BrandingResponse {
	return BrandingResponse{
		Title:       b.Title,
		Banner:      b.Banner,
		Logo:        b.LogoDataURI,
		FooterText:  b.FooterText,
		CompanyName: b.CompanyName,
		CompanyURL:  b.CompanyURL,
		LegalNotice: b.LegalNotice,
	}
}

func newLDAPResponse(c model.LDAPConfig) LDAPResponse {
	return LDAPResponse{
		LDAPRequest: LDAPRequest{
			Enabled:        c.Enabled,
			Host:           c.Host,
			Port:           c.Port,
			StartTLS:       c.StartTLS,
			UseTLS:         c.UseTLS,
			SkipVerify:     c.SkipVerify,
			BindDN:         c.BindDN,
			BaseDN:         c.BaseDN,
			UserFilter:     c.UserFilter,
			NameAttribute:  c.NameAttribute,
			EmailAttribute: c.EmailAttribute,
			IDAttribute:    c.IDAttribute,
			DefaultRole:    c.DefaultRole,
		},
		HasPassword: c.BindPassword != "",
	}
}
