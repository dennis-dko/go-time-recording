package rest

import (
	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// ldapAdmin is the subset of the LDAP client this handler drives, kept as an
// interface so the REST layer does not depend on the client package.
type ldapAdmin struct {
	configure func(model.LDAPConfig)
	test      func(*gofr.Context, model.LDAPConfig) error
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
