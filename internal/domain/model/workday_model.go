package model

import "time"

// MinutesPerDay is how many minutes a day's clock runs through.
const MinutesPerDay = 24 * 60

// WorkDay is when somebody worked on a day - when they began, when they finished
// and how long their breaks were - as the clock on the wall read them.
//
// Beside the hours booked on projects rather than in place of them: those say
// what the time went on, and this says when it was spent, which is what a record
// of working time has to show (BAG, 13 September 2022, 1 ABR 22/21). Each person's
// own, kept for themselves: there is no view of anybody else's, as there is none
// of anybody else's hours, so this is self-documentation and not an employer's
// record.
//
// Minutes on the clock rather than moments, for the reason a stored date has no
// zone: "I began at eight" is true wherever it is read. The one day it is not the
// whole truth is a change to or from summer time, where an hour of the clock
// happens twice or not at all.
type WorkDay struct {
	UserID uint

	// Day is a calendar day, midnight UTC.
	Day time.Time

	// StartMinute is when the day began, in minutes after midnight, and EndMinute
	// when it finished - up to MinutesPerDay, the midnight that ends the day.
	StartMinute int
	EndMinute   int

	BreakMinutes int
}

// WorkedMinutes is the time between the start and the end, less the breaks.
func (w WorkDay) WorkedMinutes() int {
	return w.EndMinute - w.StartMinute - w.BreakMinutes
}
