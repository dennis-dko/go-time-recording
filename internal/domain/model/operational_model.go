package model

// Operational holds the settings an administrator may change while the
// application is running, from the Settings screen.
//
// Every field is a pointer, and nil means "whatever the environment
// configured". That distinction matters: zero is a meaningful value for
// several of these - a delete ratio of 0 switches the check off, and 0 days
// means "sweep immediately" - so a plain zero could not be told apart from
// "not set here".
//
// What is deliberately *not* in this struct is as important as what is. A
// setting belongs in the environment, not here, when getting it wrong would
// lock everyone out of the screen they would need to correct it:
//
//   - AUTH_ENABLED, because switching it off from the interface would open the
//     instance to anyone, and nobody could switch it back on.
//   - UI_ENABLED, because it removes the interface that would restore it.
//   - TLS_*, because the listener is bound at start-up and a wrong domain or
//     port makes the instance unreachable.
//   - HSTS_MAX_AGE, because a browser that has been told to refuse plain HTTP
//     keeps refusing it for as long as the value said, whatever the server
//     later serves.
//   - DB_*, which is the connection this table is read from.
type Operational struct {
	// SessionLifetimeHours is how long a sign-in lasts. Read when a session is
	// opened, so a change applies to the next sign-in rather than ending the
	// current ones.
	SessionLifetimeHours *float64 `json:"sessionLifetimeHours,omitempty"`

	// MaxDailyHours caps what any one user may book on a single day.
	MaxDailyHours *float64 `json:"maxDailyHours,omitempty"`

	// RateLimit and RateLimitWindowSeconds bound sign-in attempts and
	// token-authenticated calls per client.
	RateLimit              *int `json:"rateLimit,omitempty"`
	RateLimitWindowSeconds *int `json:"rateLimitWindowSeconds,omitempty"`

	// AutoCloseAfterDays is how old an open entry must be before the nightly
	// sweep submits it. The schedule itself stays in the environment: cron jobs
	// are registered once at start-up and cannot be re-registered live.
	AutoCloseAfterDays *int `json:"autoCloseAfterDays,omitempty"`

	// LDAPSyncMaxDeleteRatio refuses a synchronisation that would remove more
	// than this share of the directory-backed accounts. Zero switches the
	// check off, which is why the field is a pointer.
	LDAPSyncMaxDeleteRatio *float64 `json:"ldapSyncMaxDeleteRatio,omitempty"`
}

// Limits are the resolved values, with the environment supplying anything the
// administrator has not overridden.
type Limits struct {
	SessionLifetimeHours   float64
	MaxDailyHours          float64
	RateLimit              int
	RateLimitWindowSeconds int
	AutoCloseAfterDays     int
	LDAPSyncMaxDeleteRatio float64
}

// Resolve layers the stored overrides over the environment's values.
func (o Operational) Resolve(fallback Limits) Limits {
	out := fallback

	if o.SessionLifetimeHours != nil {
		out.SessionLifetimeHours = *o.SessionLifetimeHours
	}

	if o.MaxDailyHours != nil {
		out.MaxDailyHours = *o.MaxDailyHours
	}

	if o.RateLimit != nil {
		out.RateLimit = *o.RateLimit
	}

	if o.RateLimitWindowSeconds != nil {
		out.RateLimitWindowSeconds = *o.RateLimitWindowSeconds
	}

	if o.AutoCloseAfterDays != nil {
		out.AutoCloseAfterDays = *o.AutoCloseAfterDays
	}

	if o.LDAPSyncMaxDeleteRatio != nil {
		out.LDAPSyncMaxDeleteRatio = *o.LDAPSyncMaxDeleteRatio
	}

	return out
}

// InvalidOperationalFields lists the fields whose values could not be used.
//
// The bounds are what keeps an administrator from locking the instance up from
// the very screen meant to configure it: a session lifetime of a second would
// sign everyone out mid-click, and a rate limit of zero would refuse every
// sign-in including their own.
func (o Operational) InvalidOperationalFields() []string {
	var invalid []string

	if o.SessionLifetimeHours != nil &&
		(*o.SessionLifetimeHours < MinSessionLifetimeHours ||
			*o.SessionLifetimeHours > MaxSessionLifetimeHours) {
		invalid = append(invalid, "sessionLifetimeHours")
	}

	if o.MaxDailyHours != nil && (*o.MaxDailyHours <= 0 || *o.MaxDailyHours > HoursPerDay) {
		invalid = append(invalid, "maxDailyHours")
	}

	if o.RateLimit != nil && *o.RateLimit < MinRateLimit {
		invalid = append(invalid, "rateLimit")
	}

	if o.RateLimitWindowSeconds != nil &&
		(*o.RateLimitWindowSeconds < 1 || *o.RateLimitWindowSeconds > MaxRateWindowSeconds) {
		invalid = append(invalid, "rateLimitWindowSeconds")
	}

	if o.AutoCloseAfterDays != nil && *o.AutoCloseAfterDays < 0 {
		invalid = append(invalid, "autoCloseAfterDays")
	}

	if o.LDAPSyncMaxDeleteRatio != nil &&
		(*o.LDAPSyncMaxDeleteRatio < 0 || *o.LDAPSyncMaxDeleteRatio > 1) {
		invalid = append(invalid, "ldapSyncMaxDeleteRatio")
	}

	return invalid
}

// Bounds on what the Settings screen may set.
const (
	// A session shorter than this would sign people out while they work.
	MinSessionLifetimeHours = 0.25

	// Longer than a fortnight and a forgotten open tab stops being a session
	// and starts being a permanent credential.
	MaxSessionLifetimeHours = 24 * 14

	// HoursPerDay is the ceiling for a daily booking cap; more hours than a day
	// has cannot be worked in one.
	HoursPerDay = 24

	// Below this the limiter would turn away ordinary use, including the
	// administrator's own sign-in.
	MinRateLimit = 5

	// An hour-long window is already far past what rate limiting is for.
	MaxRateWindowSeconds = 3600
)
