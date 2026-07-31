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
}

// Expired reports whether the session is no longer usable.
func (s *Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}
