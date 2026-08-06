package model

import "time"

// RunningTimer is a clock somebody started and has not yet stopped.
//
// Deliberately not a Timesheet with a zero duration. A time entry has a duration,
// a status and a calendar day; a running clock has none of those yet - the day it
// belongs to is not decided until the zone and the stop time are both known - and
// forcing it into that shape would mean every total learning to skip a kind of
// entry that is not really an entry.
type RunningTimer struct {
	// UserID owns it, and is the key: one person, one clock. Two would be a
	// question nobody has an answer for - which of them the next stop belongs to.
	UserID uint

	// ProjectID is optional, the same as it is on a booking. Time can be recorded
	// first and categorised afterwards.
	ProjectID *uint

	Description *string

	// StartedAt is when the clock started, in UTC as everything stored here is.
	StartedAt time.Time
}

// Elapsed is how long the clock has been running at the given moment.
func (r RunningTimer) Elapsed(now time.Time) time.Duration {
	return now.Sub(r.StartedAt)
}

// HoursElapsed is the elapsed time as the hours a booking is measured in.
//
// Not rounded to anything. A quarter-hour grid would defeat the point of
// measuring: whoever presses stop after nineteen minutes worked nineteen minutes,
// and the column is a double.
func (r RunningTimer) HoursElapsed(now time.Time) float64 {
	return r.Elapsed(now).Hours()
}

// BookingDay is the calendar day the finished entry belongs to.
//
// The day the clock was *started*, in the user's own zone - not the day it was
// stopped. Somebody who starts at half past eleven at night and stops after
// midnight worked that evening, and an entry that lands on the following day would
// disagree with both their memory and their timesheet for the week.
func (r RunningTimer) BookingDay(zone *time.Location) time.Time {
	if zone == nil {
		zone = time.UTC
	}

	local := r.StartedAt.In(zone)

	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
}

// TimerTooShort and TimerTooLong describe a clock that cannot become an entry.
//
// The floor is the smallest bookable duration, so a clock started and stopped by
// accident does not leave a record of nothing. The ceiling is a day, which is what
// a single entry can hold - and a clock past it is almost always one somebody
// forgot rather than a shift somebody worked.
func (r RunningTimer) TimerTooShort(now time.Time) bool {
	return r.HoursElapsed(now) < MinBookableHours
}

func (r RunningTimer) TimerTooLong(now time.Time) bool {
	return r.HoursElapsed(now) > HoursPerDay
}
