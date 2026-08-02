package model

// Setting keys. They are constants because each one is read by a specific
// piece of code; a key that exists only in the database would do nothing.
const (
	SettingAppTitle     = "branding.title"
	SettingBannerText   = "branding.banner"
	SettingLogoDataURI  = "branding.logo"
	SettingFooterText   = "branding.footer"
	SettingCompanyName  = "branding.company"
	SettingCompanyURL   = "branding.companyUrl"
	SettingFooterLegal  = "branding.legal"
	SettingLDAPSettings = "ldap.config"

	// SettingTimezone is the instance-wide zone that decides which calendar day
	// a booking belongs to, for everyone who has not set their own.
	SettingTimezone = "instance.timezone"

	// SettingOperational holds the limits an administrator may change from the
	// Settings screen, layered over what the environment configured.
	SettingOperational = "instance.operational"

	// SettingSetupCompleted records that the first-run wizard was dismissed.
	// Only that: which steps are done is worked out from what is configured.
	SettingSetupCompleted = "instance.setupCompleted"

	// SettingDatasourceChosen records that a database connection was chosen
	// deliberately. The connection itself lives outside the database - it is
	// how the database is reached - so this marker is what remains readable
	// from inside it.
	SettingDatasourceChosen = "instance.datasourceChosen"
)

// Branding is the instance's own labelling, editable by the administrator.
type Branding struct {
	// Title names the browser tab and the header.
	Title string

	// Banner is an announcement shown above the application. Empty hides it.
	Banner string

	// LogoDataURI holds the logo inline, so it needs no upload directory and
	// travels with the database rather than the filesystem.
	LogoDataURI string

	FooterText  string
	CompanyName string
	CompanyURL  string
	LegalNotice string
}

// DefaultBranding is what a fresh instance shows.
func DefaultBranding() Branding {
	return Branding{Title: "Time Recording"}
}

// LDAPConfig describes how to authenticate against a directory.
type LDAPConfig struct {
	Enabled bool

	// Host and Port of the directory, e.g. ldap.example.com:389.
	Host string
	Port int

	// StartTLS upgrades a plain connection; UseTLS dials LDAPS directly.
	StartTLS bool
	UseTLS   bool

	// SkipVerify disables certificate checking. It exists for self-signed
	// test directories and is unsafe anywhere else.
	SkipVerify bool

	// BindDN and BindPassword are the service account used to search for the
	// user. Leave empty for an anonymous search.
	BindDN       string
	BindPassword string

	// BaseDN is where the search starts, UserFilter selects the account.
	// "%s" in the filter is replaced by the login name.
	BaseDN     string
	UserFilter string

	// Attributes to read the display name and mail address from.
	NameAttribute  string
	EmailAttribute string

	// IDAttribute holds an identifier that never changes for the life of an
	// account. Matching on the mail address instead would read a renamed
	// mailbox as "this person left and a new one arrived", and the
	// synchronisation would delete their recorded hours.
	//
	// OpenLDAP and most others: entryUUID. Active Directory: objectGUID,
	// which is binary and is stored hex-encoded.
	IDAttribute string

	// DefaultRole is given to accounts created on first successful sign-in.
	DefaultRole string
}

// DefaultLDAPConfig is a starting point with the usual attribute names.
func DefaultLDAPConfig() LDAPConfig {
	return LDAPConfig{
		Port:           389,
		StartTLS:       true,
		UserFilter:     "(|(uid=%s)(mail=%s)(sAMAccountName=%s))",
		NameAttribute:  "cn",
		EmailAttribute: "mail",
		IDAttribute:    "entryUUID",
		DefaultRole:    RoleEmployee,
	}
}
