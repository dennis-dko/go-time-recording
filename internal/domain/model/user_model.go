package model

import (
	"slices"
	"time"
)

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

	// HolidayRegion is the state whose public holidays this person keeps: empty
	// follows the installation, NoHolidays keeps none, and anything else is one
	// of the HolidayRegions - for somebody who works in another state than the
	// installation's, or outside Germany.
	HolidayRegion string

	// EveryDayCountsFrom is the day from which every working day counts towards
	// this person's overtime balance, booked or not, with what a holiday or an
	// absence takes off it. Nil keeps the rule every account had before there was
	// a holiday calendar - only the days booked on count - so a month already
	// behind somebody does not change under them until they choose a day.
	EveryDayCountsFrom *time.Time

	// BalanceUpperLimit and BalanceLowerLimit are the bounds this person keeps their
	// running balance within, in hours: an upper one of zero or more, a lower one of
	// zero or less. Nil is no bound, which is what everybody has until they set one.
	BalanceUpperLimit *float64
	BalanceLowerLimit *float64

	// SkipUnbookedReminder switches off the banner that says the last working day
	// has nothing booked on it. False - the reminder - is what everybody has until
	// they switch it off, which is why the field says "skip": the zero value has to
	// be the default, on a new account and on every account there already was.
	SkipUnbookedReminder bool

	// TimerAlertHours is how long a stopwatch may run before a banner says so, and
	// a notification where the browser allows one. Nil is DefaultTimerAlertHours,
	// for the reason above; zero is never.
	TimerAlertHours *int

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

// Where a balance stands against its owner's bounds.
const (
	BalanceAbove = "above"
	BalanceBelow = "below"
)

// BalanceOutside says whether a running balance lies past one of this person's
// bounds: BalanceAbove, BalanceBelow, or empty while it is inside both.
func (u *User) BalanceOutside(balance float64) string {
	switch {
	case u.BalanceUpperLimit != nil && balance > *u.BalanceUpperLimit:
		return BalanceAbove
	case u.BalanceLowerLimit != nil && balance < *u.BalanceLowerLimit:
		return BalanceBelow
	default:
		return ""
	}
}

// DefaultTimerAlertHours is how long a stopwatch runs before it is mentioned, for
// somebody who has not chosen: ten hours, the most the Working Hours Act allows in
// a day, past which a clock still running is almost always one nobody stopped.
const DefaultTimerAlertHours = 10

// MaxTimerAlertHours is the longest that can be chosen. Past a day, the stopwatch
// already says it holds more than one entry can.
const MaxTimerAlertHours = 24

// EffectiveTimerAlertHours is how long this person's stopwatch runs before it is
// mentioned: their own choice, or DefaultTimerAlertHours, and zero for never.
func (u *User) EffectiveTimerAlertHours() int {
	if u.TimerAlertHours == nil {
		return DefaultTimerAlertHours
	}

	return *u.TimerAlertHours
}
