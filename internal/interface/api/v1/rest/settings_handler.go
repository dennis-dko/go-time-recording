package rest

import (
	"runtime"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
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

	// announcements is how every open browser is told the installation has gone
	// out of service. Nil where nothing subscribes, which is every test that
	// does not ask about it.
	announcements *announce.Hub

	// running is the whole connection this process opened, not just its dialect.
	// It is what the datasource screen shows when nothing has been stored, so an
	// installation configured through the environment can see what it is
	// connected to rather than an empty form.
	running appconfig.Datasource

	// activeTelemetry is the metrics and tracing configuration this process
	// started with, for the same reason: what is stored takes effect at the next
	// start, and only this says what is happening now.
	activeTelemetry appconfig.Telemetry

	// version is the build this process was compiled from, reported alongside
	// the branding because the footer renders both and one request is better
	// than two for something on every page.
	version string

	// maintenance is dropped from cache after a save, so the switch takes effect
	// on the next request rather than within the cache interval.
	maintenance MaintenanceState

	// logLevel applies a saved log level to the running process and reports what
	// is in force. The one telemetry setting that does not wait for a restart:
	// the log sink decides what is emitted, so changing it is a store in one
	// place rather than a change to the framework's logger, which is read from
	// every request goroutine without synchronisation.
	//
	// Both nil where the process output is not captured. There is nothing
	// between the framework and the console to apply a level there, so the
	// setting keeps needing a restart and the screen keeps saying so.
	logLevel     func(string)
	runningLevel func() string
}

// WithLiveLogLevel lets a saved log level take effect without a restart.
//
// Fluent rather than a constructor parameter for the same reason
// WithMaintenance is: the sink exists before the handlers and the resolution of
// "follow the configuration file" belongs to main, which knows what the file
// said before the level was widened to capture everything.
func (h *SettingsHandler) WithLiveLogLevel(apply func(string), running func() string) *SettingsHandler {
	h.logLevel, h.runningLevel = apply, running

	return h
}

// WithAnnouncements lets the handler tell every open browser that the
// installation has gone out of service, or come back into it.
func (h *SettingsHandler) WithAnnouncements(hub *announce.Hub) *SettingsHandler {
	h.announcements = hub

	return h
}

// WithMaintenance lets the handler clear the cached maintenance state.
//
// Fluent rather than a constructor parameter because the middleware that owns the
// cache is built after the handlers, and threading it back through the
// constructor would mean building one of them twice.
func (h *SettingsHandler) WithMaintenance(state MaintenanceState) *SettingsHandler {
	h.maintenance = state

	return h
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
	activeTelemetry appconfig.Telemetry,
	version string,
	configure func(model.LDAPConfig),
	test func(*gofr.Context, model.LDAPConfig) error,
) *SettingsHandler {
	return &SettingsHandler{
		settings:        settings,
		authz:           authz,
		limits:          limits,
		activeDialect:   activeDialect,
		activeTelemetry: activeTelemetry,
		version:         version,
		ldap:            &ldapAdmin{configure: configure, test: test},
	}
}

// WithRunningConnection attaches the connection this process actually opened.
//
// The screen showed an empty form on any installation configured through the
// environment, because it is filled from the file the installer or the screen
// itself writes and a compose deployment has no such file. Above that empty form
// sat "connected via postgres", which is the running dialect - so the screen said
// it was connected and showed nothing it was connected to.
//
// Worse than looking unconfigured: the file wins over the environment, so
// somebody filling in that form would override the deployment's own settings at
// the next start, with nothing on screen saying so.
func (h *SettingsHandler) WithRunningConnection(running appconfig.Datasource) *SettingsHandler {
	h.running = running

	return h
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
	LDAPSyncMaxDeleteRatio float64 `json:"ldapSyncMaxDeleteRatio"`
}

func newOperationalLimits(l model.Limits) OperationalLimits {
	return OperationalLimits{
		SessionLifetimeHours:   l.SessionLifetimeHours,
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

// BrandingResponse is the instance labelling.
type BrandingResponse struct {
	Title string `json:"title"`

	// TabTitle names the browser tab where an installation wants it to say
	// something shorter than the header does. Empty means the title.
	TabTitle string `json:"tabTitle"`

	Banner string `json:"banner"`
	Logo   string `json:"logo"`

	// LogoHeader and LogoBanner are the logo at the size those two places draw
	// it, derived when it was saved.
	//
	// Sent instead of the original, which is a wordmark of a few hundred
	// kilobytes and went to every visitor of the sign-in screen inside this
	// answer. These are a few kilobytes each. Empty on an installation whose logo
	// predates them, where the interface falls back to the original.
	LogoHeader  string `json:"logoHeader,omitempty"`
	LogoBanner  string `json:"logoBanner,omitempty"`
	FooterText  string `json:"footerText"`
	CompanyName string `json:"companyName"`
	CompanyURL  string `json:"companyUrl"`
	LegalNotice string `json:"legalNotice"`

	// Translations carries the same four texts per language, for the interface to
	// pick from. Sent whole rather than resolved here: which language a reader
	// wants is decided in the browser - the switcher first, the browser's own
	// setting otherwise - and this endpoint answers before anyone has signed in.
	Translations map[string]BrandingTextResponse `json:"translations,omitempty"`

	// Crops carry which part of the logo each place uses, so the screen that
	// chose them can show them again. Fractions of the whole image; a place that
	// uses all of it is not listed, which is what most installations send.
	Crops map[string]CropResponse `json:"crops,omitempty"`
}

// CropResponse is a part of the logo, as fractions of the whole.
type CropResponse struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// BrandingTextResponse is one language's version of the texts.
type BrandingTextResponse struct {
	Title       string `json:"title"`
	TabTitle    string `json:"tabTitle"`
	Banner      string `json:"banner"`
	FooterText  string `json:"footerText"`
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

	// OS is what this build runs on, shown beside the version as "v1.0 (windows)".
	//
	// The same version is published for four platforms and they do not all behave
	// alike - restarting from the interface works on Linux and cannot on Windows -
	// so "which version" is only half of what a support conversation needs. Public
	// for the same reason the version is: it is on a page anyone can reach, and an
	// installation that depends on nobody knowing its platform was not safe anyway.
	OS string `json:"os"`
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
		OS:               runtime.GOOS,
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
		TabTitle:    req.TabTitle,
		Banner:      req.Banner,
		LogoDataURI: req.Logo,
		FooterText:  req.FooterText,
		CompanyName: req.CompanyName,
		CompanyURL:  req.CompanyURL,
		LegalNotice: req.LegalNotice,
		HeaderCrop:  cropFrom(req.Crops["header"]),
		BannerCrop:  cropFrom(req.Crops["banner"]),
		IconCrop:    cropFrom(req.Crops["icon"]),
		Translations: func() map[string]model.BrandingText {
			if len(req.Translations) == 0 {
				return nil
			}

			out := make(map[string]model.BrandingText, len(req.Translations))

			for language, text := range req.Translations {
				// Only languages the interface has words for. A translation for a
				// language nothing can select is a row nobody will ever read, and
				// this is the one place a caller names the key.
				if !model.IsSupportedLanguage(language) {
					continue
				}

				out[language] = model.BrandingText{
					Title:       text.Title,
					TabTitle:    text.TabTitle,
					Banner:      text.Banner,
					FooterText:  text.FooterText,
					LegalNotice: text.LegalNotice,
				}
			}

			return out
		}(),
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
		OS:               runtime.GOOS,
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

	// SyncSchedule is the cron expression for the automatic reconciliation, or
	// empty for manual only. Unlike the rest of this payload it applies at the
	// next start.
	SyncSchedule string `json:"syncSchedule"`
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

	if err := h.requireAdminForSchedule(c, config); err != nil {
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
		// as a readable message rather than a 500. The same shape the database
		// probe uses, though what reaches here is almost always the directory's
		// own words - a bind refused, a name that does not resolve - because a
		// configuration this cannot use was already refused by bindLDAP above,
		// as a proper error, translated like any other.
		return map[string]any{"ok": false, "error": probeFailure(err)}, nil
	}

	// No message on the way out. What a success is called is the interface's
	// business and it has a translated sentence for it; this one was written
	// here in English and shown in preference to it, so a German screen
	// answered "connection established". A failure still carries its own text,
	// because what went wrong is not a fixed set of sentences code could
	// translate.
	return map[string]any{"ok": true}, nil
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
		SyncSchedule:   req.SyncSchedule,
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

	// Stored says whether the fields above came from anywhere. False on an
	// installation configured through the environment, where the screen has
	// nothing of its own to show and used to show nothing at all.
	Stored bool `json:"stored"`

	// Running is the connection this process opened, so a screen with nothing
	// stored can show what is in force instead of an empty form. No password:
	// the screen never receives one, and this is not the place to start.
	Running DatasourceRequest `json:"running"`

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

	resp := DatasourceResponse{
		Active: h.activeDialect,
		Stored: ok,
		Running: DatasourceRequest{
			Dialect: h.running.Dialect,
			Name:    h.running.Name,
			Host:    h.running.Host,
			Port:    h.running.Port,
			User:    h.running.User,
			SSLMode: h.running.SSLMode,
		},
	}

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
		return nil, toHTTPError(apperror.Invalidf("%v", err).WithCode("datasourceInvalid"))
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, ds); err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	// No message. There used to be one, written in English here, and the interface
	// showed it in preference to its own translated sentence - so the one screen
	// that is otherwise entirely German answered a successful save in English.
	// What to call this is the interface's business; what happened is this one's.
	return map[string]any{
		"status":          "saved",
		"restartRequired": true,
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
		// A failed probe is information, not a server fault - so it comes back
		// as a 200 with the reason in it rather than as an error status.
		//
		// The reason travels the way a refusal does, because half of these are
		// refusals: a field left empty is a fixed complaint the interface can
		// name and translate, and only what the driver says back - "connection
		// refused", "password authentication failed" - is prose nobody can
		// anticipate. Both arrive here; the client shows whichever it can.
		return map[string]any{"ok": false, "error": probeFailure(err)}, nil
	}

	// No message on the way out. What a success is called is the interface's
	// business and it has a translated sentence for it; this one was written
	// here in English and shown in preference to it, so a German screen
	// answered "connection established". A failure still carries its own text,
	// because what went wrong is not a fixed set of sentences code could
	// translate.
	return map[string]any{"ok": true}, nil
}

// requireAdmin restricts the screen to the built-in administrator: these
// settings decide where the data lives and who may sign in at all.
func (h *SettingsHandler) requireAdmin(c *gofr.Context) error {
	_, err := h.authz.RequireInstallationAdmin(c)

	return err
}

// requireAdminForSchedule keeps the automatic directory run with the account that
// may perform one by hand.
//
// Running the synchronisation is the built-in administrator's alone, because it
// deletes the accounts the directory no longer holds and everything they
// recorded. Scheduling it is the same act performed later and unattended, and it
// was open to anybody holding settings:manage - so the safety the button was
// given could be walked around by typing five numbers into the field beside it.
//
// Only a change is refused. The schedule travels with the rest of the directory
// settings, so somebody editing the connection sends the stored value back
// unchanged, and refusing that would refuse them the connection form as well.
func (h *SettingsHandler) requireAdminForSchedule(c *gofr.Context, wanted model.LDAPConfig) error {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return err
	}

	if h.authz.AdministersOnly(principal) {
		return nil
	}

	stored, err := h.settings.LDAP(c)
	if err != nil {
		return toHTTPError(err)
	}

	if wanted.SyncSchedule == stored.SyncSchedule {
		return nil
	}

	return forbiddenError{
		msg: "only an administrator of this installation may schedule the directory " +
			"synchronisation",
	}.WithCode("onlyBuiltInAdminSchedules")
}

func newBrandingResponse(b model.Branding) BrandingResponse {
	return BrandingResponse{
		Title:       b.Title,
		TabTitle:    b.TabTitle,
		Banner:      b.Banner,
		Logo:        b.LogoDataURI,
		LogoHeader:  b.LogoHeader,
		LogoBanner:  b.LogoBanner,
		Crops:       cropsOf(b),
		FooterText:  b.FooterText,
		CompanyName: b.CompanyName,
		CompanyURL:  b.CompanyURL,
		LegalNotice: b.LegalNotice,
		Translations: func() map[string]BrandingTextResponse {
			if len(b.Translations) == 0 {
				return nil
			}

			out := make(map[string]BrandingTextResponse, len(b.Translations))

			for language, text := range b.Translations {
				out[language] = BrandingTextResponse{
					Title:       text.Title,
					TabTitle:    text.TabTitle,
					Banner:      text.Banner,
					FooterText:  text.FooterText,
					LegalNotice: text.LegalNotice,
				}
			}

			return out
		}(),
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
			SyncSchedule:   c.SyncSchedule,
		},
		HasPassword: c.BindPassword != "",
	}
}

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

// TelemetryResponse carries the administered metrics and tracing settings
// together with what this process is actually doing.
//
// Both, because they disagree until the next restart, and a screen that showed
// only the stored values would report tracing as configured while no span had
// been exported since it was saved.
type TelemetryResponse struct {
	// Configured holds only what has been administered; an absent field follows
	// the configuration file.
	Configured model.Telemetry `json:"configured"`

	// Active is what this process is serving and exporting right now.
	Active ActiveTelemetry `json:"active"`

	// RestartRequired is true on a save: GoFr binds the metrics port and builds
	// the trace exporter at start-up, so nothing here can take effect sooner.
	RestartRequired bool `json:"restartRequired"`
}

// ActiveTelemetry is the telemetry in force in this process, on the wire.
type ActiveTelemetry struct {
	LogLevel string `json:"logLevel"`

	MetricsServed bool   `json:"metricsServed"`
	MetricsPort   int    `json:"metricsPort"`
	MetricsPath   string `json:"metricsPath"`

	// TraceExporter is empty when spans go nowhere, which is the default.
	TraceExporter string  `json:"traceExporter"`
	TracerURL     string  `json:"tracerUrl"`
	TracerRatio   float64 `json:"tracerRatio"`
}

func newActiveTelemetry(t appconfig.Telemetry) ActiveTelemetry {
	return ActiveTelemetry{
		LogLevel:      t.LogLevel,
		MetricsServed: t.MetricsServed(),
		MetricsPort:   t.MetricsPort,
		MetricsPath:   appconfig.MetricsPath,
		TraceExporter: t.TraceExporter,
		TracerURL:     t.TracerURL,
		TracerRatio:   t.TracerRatio,
	}
}

// Telemetry handles GET /api/v1/settings/telemetry.
func (h *SettingsHandler) Telemetry(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	configured, err := h.settings.Telemetry(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return TelemetryResponse{
		Configured: configured,
		Active:     h.active(),
	}, nil
}

// SaveTelemetry handles PUT /api/v1/settings/telemetry.
//
// The settings are written to the database and read back out of it before
// gofr.New() on the next start; they are deliberately not applied to the running
// process. Switching the metrics listener or replacing the trace provider under
// live requests would mean reimplementing what GoFr does at start-up and mutating
// a global while it is in use, for a convenience nobody asked for.
func (h *SettingsHandler) SaveTelemetry(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req model.Telemetry
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	if err := h.settings.SaveTelemetry(c, req); err != nil {
		return nil, toHTTPError(err)
	}

	// Read back rather than echo: the collector address is trimmed on the way in,
	// and an exporter of "off" clears it, so echoing the request would show a
	// setting the next start is not going to use.
	stored, err := h.settings.Telemetry(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// In force from the next line written, rather than from the next start. An
	// empty value means "follow the configuration file", which is a value only
	// main knows - so it is passed through as the empty string and resolved
	// there.
	if h.logLevel != nil {
		level := ""
		if stored.LogLevel != nil {
			level = *stored.LogLevel
		}

		h.logLevel(level)
	}

	return TelemetryResponse{
		Configured: stored,
		Active:     h.active(),

		// The log level is exempt now, so a save that changed only that needs
		// nothing further. Everything else here is still built inside gofr.New().
		RestartRequired: h.logLevel == nil || !onlyLogLevelChanged(stored, h.activeTelemetry),
	}, nil
}

// onlyLogLevelChanged reports whether a save left everything that needs a
// restart exactly as the running process has it.
func onlyLogLevelChanged(stored model.Telemetry, running appconfig.Telemetry) bool {
	if stored.MetricsOff && running.MetricsServed() {
		return false
	}

	if stored.TraceExporter != nil && *stored.TraceExporter != running.TraceExporter {
		return false
	}

	if stored.TracerURL != nil && *stored.TracerURL != running.TracerURL {
		return false
	}

	if stored.TracerRatio != nil && *stored.TracerRatio != running.TracerRatio {
		return false
	}

	return true
}

// active is what this process is doing now, with the log level read live where
// it can be: that one is no longer whatever the process started with.
func (h *SettingsHandler) active() ActiveTelemetry {
	out := newActiveTelemetry(h.activeTelemetry)

	if h.runningLevel != nil {
		if level := h.runningLevel(); level != "" {
			out.LogLevel = level
		}
	}

	return out
}

// cropsOf is what the screen needs to show the parts that were chosen.
//
// Named rather than positional, and only the ones that are not the whole image:
// a logo nobody has cropped answers with nothing at all, which is what the great
// majority of installations will send and receive.
func cropsOf(b model.Branding) map[string]CropResponse {
	out := map[string]CropResponse{}

	for name, crop := range map[string]model.LogoCrop{
		"header": b.HeaderCrop,
		"banner": b.BannerCrop,
		"icon":   b.IconCrop,
	} {
		if crop.Whole() {
			continue
		}

		out[name] = CropResponse{X: crop.X, Y: crop.Y, W: crop.W, H: crop.H}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// cropFrom reads one back. An absent one is the whole image, which is both the
// default and the answer for anything that makes no sense - the scaler clamps
// what it is given rather than trusting it.
func cropFrom(c CropResponse) model.LogoCrop {
	return model.LogoCrop{X: c.X, Y: c.Y, W: c.W, H: c.H}
}
