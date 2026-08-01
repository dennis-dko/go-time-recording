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
	return Branding{Title: "Zeiterfassung"}
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
		DefaultRole:    RoleEmployee,
	}
}
