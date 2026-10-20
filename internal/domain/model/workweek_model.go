package model

import "time"

// WeekHours are the hours each weekday asks of somebody, Monday first.
//
// Monday first because that is how a working week is read here and how the
// calendar draws it. time.Weekday counts from Sunday, so the index is turned
// round in one place, On, rather than at every reader.
type WeekHours [7]float64

// On is what the week asks of the given day.
func (w WeekHours) On(day time.Time) float64 {
	return w[(int(day.Weekday())+6)%7]
}

// Valid reports whether every day of the week is an amount of hours a day can
// hold. Written as "inside the range" so that NaN, which compares false with
// everything, is outside it.
func (w WeekHours) Valid() bool {
	for _, hours := range w {
		if !(hours >= 0 && hours <= HoursPerDay) {
			return false
		}
	}

	return true
}

// DefaultWeek is the week of somebody who has not planned one: their daily
// target from Monday to Friday, and nothing at the weekend.
//
// The weekend is the point. Every booked day used to be measured against the
// full daily target whichever day it was, so four hours worked on a Saturday
// stood in the balance as four hours short - the overtime somebody had just done
// counted against them.
func DefaultWeek(dailyTarget float64) WeekHours {
	return WeekHours{dailyTarget, dailyTarget, dailyTarget, dailyTarget, dailyTarget, 0, 0}
}

// WorkSchedule is a planned week, in force from a day until the next one is.
//
// From a day rather than for ever: a change - to part time, or back from it - is
// a new schedule, and the months before it go on being measured against the week
// that was planned then rather than rewritten by the one planned now.
type WorkSchedule struct {
	UserID uint

	// ValidFrom is the first day it applies to, a calendar day like every date
	// stored here - see CalendarDay.
	ValidFrom time.Time

	Hours WeekHours
}

// TargetOn is what a day asks of somebody: the week of the schedule in force on
// it, or before where no schedule is in force yet.
//
// schedules must be ordered by ValidFrom, earliest first, which is the order the
// repository returns them in.
func TargetOn(day time.Time, schedules []WorkSchedule, before WeekHours) float64 {
	week := before

	for _, schedule := range schedules {
		if schedule.ValidFrom.After(day) {
			break
		}

		week = schedule.Hours
	}

	return week.On(day)
}
