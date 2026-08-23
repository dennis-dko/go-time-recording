package model

import "time"

// Session is a signed-in browser session.
//
// Only TokenHash is stored: the token itself lives in the client's cookie, so
// a leaked database cannot be replayed as a live session.
type Session struct {
	TokenHash string
	UserID    uint
	CreatedAt time.Time
	ExpiresAt time.Time

	// LastSeenAt is when this session was last used, which is what an idle
	// timeout measures against. Set when the session is opened, so a session
	// nobody ever uses is as idle as one abandoned after a day's work.
	LastSeenAt time.Time
}

// Expired reports whether the session has run out of its own lifetime.
//
// The lifetime is absolute and starts at the sign-in: it is a bound on how long
// one act of proving who you are is worth, and it does not move because somebody
// kept working. Idle is the other question - see Idle.
func (s *Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// Idle reports whether the session has gone unused for longer than allowed.
//
// A timeout of zero is no timeout, which is what an installation gets until
// somebody sets one: switching people out of a screen they left open is a
// decision about how an office works rather than a default worth imposing on
// every installation on the day it updates.
//
// Measured from the last use rather than from the sign-in, which is the whole
// difference between this and the lifetime above: a person working all morning
// keeps their session, and the same person going home at noon loses it whether
// or not the lifetime has run out.
func (s *Session) Idle(now time.Time, timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}

	return now.Sub(s.LastSeenAt) > timeout
}
