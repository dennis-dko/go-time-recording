package model

import "time"

// CalendarFeed is the address somebody's calendar program reads their recorded days
// from: their booked hours a day at a time, and the days they are away.
//
// It is reached by a key of its own rather than by an API token, because a
// subscription carries its secret in the address, and an address is written into
// the request log of everything it passes through. A token there would be the
// whole of its owner's role; this key reads one person's days and nothing else.
//
// One per person. A second address would be a second secret to keep for the same
// days, and replacing the one is how a key somebody has seen is taken back.
type CalendarFeed struct {
	ID     uint
	UserID uint

	// KeyHash is all that is kept of the key: the key itself exists once, in the
	// answer that created it, as an API token's secret does.
	KeyHash string

	// Prefix is the start of the key, enough for its owner to recognise which
	// address their calendar was given.
	Prefix string

	CreatedAt  time.Time
	LastReadAt *time.Time
}
