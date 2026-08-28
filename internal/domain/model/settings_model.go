package model

import (
	"fmt"
	"strconv"
	"strings"
)

// Setting keys. They are constants because each one is read by a specific
// piece of code; a key that exists only in the database would do nothing.
const (
	SettingAppTitle    = "branding.title"
	SettingTabTitle    = "branding.tabTitle"
	SettingBannerText  = "branding.banner"
	SettingLogoDataURI = "branding.logo"

	// The logo at the size each place shows it, derived when it is saved.
	//
	// Kept beside the original rather than instead of it: the original is what a
	// later version re-derives from when a size changes, and it is the only copy
	// that has lost nothing. What is sent to a browser is these.
	SettingLogoHeader = "branding.logo.header"
	SettingLogoBanner = "branding.logo.banner"
	SettingLogoIcon   = "branding.logo.icon"

	// Which part of the logo each place uses, as fractions of the whole.
	//
	// A wordmark that reads as a banner is a smear in a tab, and the part worth
	// keeping there is usually the mark at one end rather than the middle. Nobody
	// can guess which, so it is chosen - and remembered per place, because the
	// answer differs between a wide header and a square tab.
	SettingLogoHeaderCrop = "branding.logo.header.crop"
	SettingLogoBannerCrop = "branding.logo.banner.crop"
	SettingLogoIconCrop   = "branding.logo.icon.crop"
	SettingFooterText     = "branding.footer"
	SettingCompanyName    = "branding.company"
	SettingCompanyURL     = "branding.companyUrl"
	SettingFooterLegal    = "branding.legal"
	SettingLDAPSettings   = "ldap.config"

	// SettingSecretKeyCheck is a value sealed with the configured key, kept so
	// the application can tell at start-up whether the key it has is the key its
	// data was written with.
	//
	// Without it, the wrong key is discovered by whoever tries to sign in with a
	// second factor, days later, as "your code is wrong". With it, the process
	// refuses to start and says which of the two is out of step.
	SettingSecretKeyCheck = "instance.secretKeyCheck"

	// SettingTimezone is the instance-wide zone that decides which calendar day
	// a booking belongs to, for everyone who has not set their own.
	SettingTimezone = "instance.timezone"

	// SettingOperational holds the limits an administrator may change from the
	// Settings screen, layered over what the environment configured.
	SettingOperational = "instance.operational"

	// SettingMaintenance takes the installation out of service without stopping
	// the process, so people cannot record time against a database that is about
	// to be restored or moved.
	SettingMaintenance = "instance.maintenance"

	// SettingTelemetry holds the metrics and tracing settings. Read before
	// gofr.New(), because that is where GoFr decides both, and therefore applied
	// at the next start rather than at once.
	SettingTelemetry = "instance.telemetry"

	// SettingSetupCompleted records that the first-run wizard was dismissed.
	// Only that: which steps are done is worked out from what is configured.
	SettingSetupCompleted = "instance.setupCompleted"
)

// Branding is the instance's own labelling, editable by the administrator.
type Branding struct {
	// Title names the header, and the browser tab where TabTitle is empty.
	Title string

	// TabTitle names the browser tab on its own, where an installation wants it
	// to say something different.
	//
	// The two were one field, which is the right answer until the header's name
	// is too long to be a tab: a tab is a couple of dozen characters wide before
	// the browser cuts it off, and several of an installation's tabs are usually
	// open at once. "Zeiterfassung der Beispiel GmbH & Co. KG" belongs across the
	// top of the screen; the tab wants "Zeiterfassung".
	//
	// Empty means the title, so an installation that has never thought about this
	// keeps what it had.
	TabTitle string

	// Banner is an announcement shown above the application. Empty hides it.
	Banner string

	// LogoDataURI holds the logo inline, so it needs no upload directory and
	// travels with the database rather than the filesystem.
	//
	// The original, exactly as uploaded. Nothing displays it: each place is shown
	// one of the three below, derived from this when it was saved. It is kept so
	// that changing a size later is a re-derivation rather than asking every
	// installation to upload their logo again.
	LogoDataURI string

	// LogoHeader, LogoBanner and LogoIcon are the logo at the size the header,
	// the sign-in card and a browser tab draw it. Empty on an installation whose
	// logo was saved before these existed, which is why every reader falls back
	// to LogoDataURI.
	LogoHeader string
	LogoBanner string
	LogoIcon   string

	// The part of the logo each of those is made from. The zero value is the
	// whole image, which is what a logo starts as.
	HeaderCrop LogoCrop
	BannerCrop LogoCrop
	IconCrop   LogoCrop

	FooterText  string
	CompanyName string
	CompanyURL  string
	LegalNotice string

	// Translations carries the same four texts in the languages this
	// installation has written them in, keyed by language.
	//
	// The fields above stay the ones an installation is asked for first and the
	// answer for any language nobody has written: a company that works in one
	// language fills those in and never opens this, and a reader whose language
	// has no translation sees them rather than nothing.
	//
	// Only the texts. The logo, the company's address and the URL are the same
	// in every language - translating a link would be translating where it goes.
	Translations map[string]BrandingText
}

// TabName is what the browser tab is called.
//
// The one place that asks this question rather than reading either field, so
// nothing has to remember which of the two wins. The tab title where there is
// one, and the ordinary title otherwise - which is every installation that has
// never opened this setting.
// TitleIn is what this installation calls itself in one language.
//
// The footer of an exported document is the one place the server writes a word
// of its own into something otherwise composed entirely on the screen, so it is
// the one place that has to be told which language is reading - a German
// evaluation used to be signed off in English.
//
// Falls back to the untranslated title, and then to nothing, so an installation
// that has named itself in one language only is still named.
func (b Branding) TitleIn(language string) string {
	if text, ok := b.Translations[language]; ok {
		if title := strings.TrimSpace(text.Title); title != "" {
			return title
		}
	}

	return strings.TrimSpace(b.Title)
}

func (b Branding) TabName() string {
	if tab := strings.TrimSpace(b.TabTitle); tab != "" {
		return tab
	}

	return b.Title
}

// BrandingText is what an installation says about itself, in one language.
type BrandingText struct {
	Title       string
	TabTitle    string
	Banner      string
	FooterText  string
	LegalNotice string
}

// brandingTextKeys are the setting names one language's texts are stored under.
//
// Suffixed rather than prefixed, so everything about the appearance still sorts
// together in a table somebody is reading by eye.
func brandingTextKeys(language string) BrandingTextKeys {
	return BrandingTextKeys{
		Title:       SettingAppTitle + "." + language,
		TabTitle:    SettingTabTitle + "." + language,
		Banner:      SettingBannerText + "." + language,
		FooterText:  SettingFooterText + "." + language,
		LegalNotice: SettingFooterLegal + "." + language,
	}
}

// BrandingTextKeys names where one language's texts live.
type BrandingTextKeys struct {
	Title       string
	TabTitle    string
	Banner      string
	FooterText  string
	LegalNotice string
}

// BrandingKeysFor is brandingTextKeys, exported for the settings service.
func BrandingKeysFor(language string) BrandingTextKeys { return brandingTextKeys(language) }

// FallbackAppTitle is the name an instance carries when nothing else has been
// configured - neither a title under Settings nor APP_NAME in the environment.
const FallbackAppTitle = "Time Recording"

// DefaultBranding is what a fresh instance shows.
//
// The title comes from APP_NAME when the deployment set one, so an operator who
// names their instance in the environment sees that name, rather than having to
// discover that the variable only fed the two-factor issuer and set it a second
// time under Settings.
func DefaultBranding(appName string) Branding {
	if appName == "" {
		appName = FallbackAppTitle
	}

	return Branding{Title: appName}
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

	// SyncSchedule is when the reconciliation runs on its own, as a five-field
	// cron expression. Empty means it only runs when somebody presses the button,
	// which is the default: a run deletes the accounts the directory no longer
	// holds, together with every hour recorded against them.
	//
	// Unlike the rest of this struct it takes effect at the next start, not at
	// once - cron jobs are registered while the application starts and cannot be
	// re-registered underneath a running scheduler. The screen says so, and the
	// restart card lists it while it waits.
	SyncSchedule string
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
		DefaultRole:    RoleUser,
	}
}

// LogoCrop is the part of the logo one place uses, as fractions of the whole.
//
// Fractions rather than pixels, because it is chosen against the image as the
// browser drew it and applied to the original: two different sizes, neither of
// which the other should have to know.
type LogoCrop struct {
	X, Y, W, H float64
}

// Whole reports whether this selects the entire image, which is the default.
func (c LogoCrop) Whole() bool {
	return c.W <= 0 || c.H <= 0
}

// String is how a crop is stored: four numbers, or empty for the whole image.
//
// Its own small format rather than JSON in a settings value, because the settings
// table holds strings people read when something has gone wrong, and
// "0.00,0.10,0.35,0.80" is legible in a way an escaped object is not.
func (c LogoCrop) String() string {
	if c.Whole() {
		return ""
	}

	return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", c.X, c.Y, c.W, c.H)
}

// ParseLogoCrop reads what String wrote. Anything unreadable is the whole image,
// which is the safe answer: a logo shown complete is never wrong, only sometimes
// small.
func ParseLogoCrop(raw string) LogoCrop {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	if len(parts) != 4 {
		return LogoCrop{}
	}

	values := make([]float64, 4)

	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return LogoCrop{}
		}

		values[i] = value
	}

	return LogoCrop{X: values[0], Y: values[1], W: values[2], H: values[3]}
}
