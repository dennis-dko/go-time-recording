package model

// Legacy role names, kept as the names of the seeded default roles so existing
// data and configuration keep working after roles moved into the database.
const (
	UserRoleAdmin    = RoleAdmin
	UserRoleEmployee = RoleEmployee
)

// Defaults applied to a user who has not chosen their own working times.
const (
	DefaultDailyTargetHours = 8
	DefaultMaxDailyHours    = 12
)

// User model
type User struct {
	ID    uint
	Name  string
	Email string

	// RoleID is the authoritative link; RoleName is filled in when reading for
	// display and is never written back.
	RoleID   uint
	RoleName string

	// PasswordHash is a bcrypt hash. It is never included in an API response.
	PasswordHash string

	// MustChangePassword marks a still-unchanged initial password.
	MustChangePassword bool

	// IsSystem marks the built-in administrator, which cannot be deleted or
	// locked out of its role.
	IsSystem bool

	// DailyTargetHours is the expected working time per day and the basis for
	// the overtime balance. MaxDailyHours caps what may be booked on one day.
	DailyTargetHours float64
	MaxDailyHours    float64

	// TOTPSecret is set while enrolling; TOTPEnabled only becomes true once
	// the user has proven they can generate a code, so a half-finished setup
	// never locks anyone out.
	TOTPSecret  string
	TOTPEnabled bool

	// Language is the interface language; empty means the default.
	Language string

	// TourSeen records that this person has been shown the guided tour. Per
	// user rather than per device: it answers "has this person been introduced
	// to the application", which does not become false again on a new laptop.
	TourSeen bool

	// Timezone is an IANA name such as "Europe/Berlin". Empty means "use the
	// instance setting", which is the common case: only someone working from
	// another country needs their own.
	//
	// It decides which calendar day a booking falls on, so getting it wrong
	// moves hours between days rather than merely displaying them oddly.
	Timezone string

	// IsExternal marks an account whose password lives in LDAP, so the local
	// password fields are not used for it.
	IsExternal bool

	// ExternalID is the directory-side identifier that never changes. It is
	// what the synchronisation matches on: matching by mail address would read
	// a renamed mailbox as a departure and delete the person's recorded hours.
	ExternalID string
}

// Supported interface languages.
const (
	LanguageEnglish = "en"
	LanguageGerman  = "de"

	// DefaultLanguage is what an account without a stored choice gets, and what
	// an unrecognised choice falls back to. English, because that is the
	// language the interface itself is written in: every other language is a
	// translation layered over it, so a gap in one still reads.
	DefaultLanguage = LanguageEnglish
)

// SupportedLanguages lists the languages the interface ships translations for,
// the fallback first.
func SupportedLanguages() []string {
	return []string{LanguageEnglish, LanguageGerman}
}

// IsSupportedLanguage reports whether the interface has translations for it.
func IsSupportedLanguage(language string) bool {
	for _, supported := range SupportedLanguages() {
		if supported == language {
			return true
		}
	}

	return false
}

// EffectiveLanguage returns the user's language, falling back to the default.
func (u *User) EffectiveLanguage() string {
	if !IsSupportedLanguage(u.Language) {
		return DefaultLanguage
	}

	return u.Language
}

// EffectiveDailyTarget returns the user's target, falling back to the default
// so an unconfigured user still gets a meaningful overtime balance.
func (u *User) EffectiveDailyTarget() float64 {
	if u.DailyTargetHours <= 0 {
		return DefaultDailyTargetHours
	}

	return u.DailyTargetHours
}

// EffectiveMaxDaily returns the user's daily cap, falling back to fallback
// (the instance-wide limit) when the user has not set one.
func (u *User) EffectiveMaxDaily(fallback float64) float64 {
	if u.MaxDailyHours <= 0 {
		return fallback
	}

	return u.MaxDailyHours
}
