package model

import "slices"

// DefaultDailyTargetHours is the target applied to a user who has not chosen
// their own.
//
// There was a DefaultMaxDailyHours beside it, and it is gone with the method that
// was its only reader. A default daily *cap* was the wrong shape for the rule
// this application actually has: the ceiling belongs to the installation, and a
// personal figure may only make somebody's own day shorter than it. See
// TimesheetApplicationService.dailyLimitFor, which takes the stricter of the two.
const DefaultDailyTargetHours = 8

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

	// Theme is light or dark, or empty for following the time of day.
	//
	// On the account rather than on the device, which is where it used to be.
	// A device is shared and an account is not: the next person to sit down at a
	// machine got the last one's dark mode, on a screen with nothing else of
	// theirs on it. Empty is the honest default for somebody who has never
	// chosen, and is what a signed-out screen always uses.
	Theme string

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

// The appearances an account may choose. Empty is the third and is not listed:
// it is the absence of a choice rather than one of them.
const (
	ThemeLight = "light"
	ThemeDark  = "dark"
)

// IsSupportedTheme reports whether this is an appearance the interface can show.
//
// Empty passes, because clearing the choice is how somebody goes back to
// following the time of day - the same shape as an empty timezone meaning
// "follow the instance".
func IsSupportedTheme(theme string) bool {
	return theme == "" || theme == ThemeLight || theme == ThemeDark
}

// SupportedLanguages lists the languages the interface ships translations for,
// the fallback first.
func SupportedLanguages() []string {
	return []string{LanguageEnglish, LanguageGerman}
}

// IsSupportedLanguage reports whether the interface has translations for it.
func IsSupportedLanguage(language string) bool {
	return slices.Contains(SupportedLanguages(), language)
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
