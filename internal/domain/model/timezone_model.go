package model

import "time"

// DefaultTimezone is what an instance uses until an administrator picks one.
//
// UTC rather than the server's own zone: a server that is moved, or a container
// image rebuilt on a differently configured host, would otherwise silently
// change which calendar day people's hours land on.
const DefaultTimezone = "UTC"

// IsSupportedTimezone reports whether the name is one the runtime knows.
//
// The check is a real lookup rather than a pattern match, because a plausible
// but wrong name like "Europe/Munich" would otherwise be stored and then fall
// back to UTC at every use, quietly shifting bookings by an hour.
func IsSupportedTimezone(name string) bool {
	if name == "" {
		return false
	}

	_, err := time.LoadLocation(name)

	return err == nil
}

// EffectiveTimezone resolves which zone applies, preferring the user's own.
//
// Both are validated on the way in, but a zone can also stop being valid: a
// database restored onto an older build, or a name removed from the tz
// database, would both arrive here. Falling back is what keeps the application
// working in that case, rather than failing a request over a display detail.
func EffectiveTimezone(userTimezone, instanceTimezone string) *time.Location {
	for _, candidate := range []string{userTimezone, instanceTimezone, DefaultTimezone} {
		if candidate == "" {
			continue
		}

		if location, err := time.LoadLocation(candidate); err == nil {
			return location
		}
	}

	return time.UTC
}

// EffectiveTimezoneName is EffectiveTimezone's answer as a name, for handing to
// a client that does its own date arithmetic.
func EffectiveTimezoneName(userTimezone, instanceTimezone string) string {
	return EffectiveTimezone(userTimezone, instanceTimezone).String()
}

// TimezoneOf returns the zone this user's days are counted in.
func (u *User) TimezoneOf(instanceTimezone string) *time.Location {
	return EffectiveTimezone(u.Timezone, instanceTimezone)
}
