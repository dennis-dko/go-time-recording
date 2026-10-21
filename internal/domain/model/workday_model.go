package model

import (
	"slices"
	"time"
)

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

// The limits of the Working Hours Act (Arbeitszeitgesetz) a recorded day can be
// read against, in the order of its sections: no more than ten hours a day
// (section 3), a break that grows with the hours (section 4), and eleven hours of
// rest before the next shift (section 5).
const (
	NoticeLongDay    = "longDay"
	NoticeShortBreak = "shortBreak"
	NoticeShortRest  = "shortRest"
)

const (
	longestShiftMinutes = 10 * 60
	shortestRestMinutes = 11 * 60
)

// WorkingTimeNotice is one limit of the Working Hours Act a shift went past.
//
// Minutes is the figure it is about: the minutes worked for NoticeLongDay, the
// break the hours called for for NoticeShortBreak, and the rest since the shift
// before for NoticeShortRest.
type WorkingTimeNotice struct {
	Rule    string
	Minutes int
}

// WorkingTimeNotices reads recorded days against the Working Hours Act, and
// returns what it says about each shift under the day the shift began.
//
// A notice and not a refusal: the act has exceptions this application cannot
// know about - a hospital, a farm at harvest, a collective agreement - and a
// record of working time that refused the truth would be a worse record.
//
// A night that runs past midnight is recorded as the two days it falls on, so a
// day that ends at midnight and a next day that begins at it are one shift: eight
// hours from ten at night to six in the morning are a shift that needed a break,
// not two short days with no rest between them.
func WorkingTimeNotices(days []WorkDay) map[time.Time][]WorkingTimeNotice {
	notices := make(map[time.Time][]WorkingTimeNotice)

	var before *shift

	for _, current := range shiftsOf(days) {
		var said []WorkingTimeNotice

		if current.worked > longestShiftMinutes {
			said = append(said, WorkingTimeNotice{Rule: NoticeLongDay, Minutes: current.worked})
		}

		if needed := breakNeeded(current.worked); current.breaks < needed {
			said = append(said, WorkingTimeNotice{Rule: NoticeShortBreak, Minutes: needed})
		}

		if before != nil {
			rest := int(current.startDay.Sub(before.endDay).Hours())/24*MinutesPerDay +
				current.startMinute - before.endMinute

			if rest < shortestRestMinutes {
				said = append(said, WorkingTimeNotice{Rule: NoticeShortRest, Minutes: rest})
			}
		}

		if len(said) > 0 {
			notices[current.startDay] = said
		}

		before = &current
	}

	return notices
}

// breakNeeded is the break section 4 asks of a shift: thirty minutes past six
// hours of work, forty-five past nine.
func breakNeeded(workedMinutes int) int {
	switch {
	case workedMinutes > 9*60:
		return 45
	case workedMinutes > 6*60:
		return 30
	default:
		return 0
	}
}

// shift is one stretch of work, which is one recorded day or a run of them joined
// at midnight.
type shift struct {
	startDay, endDay       time.Time
	startMinute, endMinute int
	worked, breaks         int
}

// shiftsOf joins recorded days into shifts, in calendar order.
func shiftsOf(days []WorkDay) []shift {
	sorted := slices.Clone(days)
	slices.SortFunc(sorted, func(a, b WorkDay) int { return a.Day.Compare(b.Day) })

	var shifts []shift

	for _, day := range sorted {
		if n := len(shifts); n > 0 {
			last := &shifts[n-1]

			if last.endMinute == MinutesPerDay && day.StartMinute == 0 &&
				day.Day.Equal(last.endDay.AddDate(0, 0, 1)) {
				last.endDay, last.endMinute = day.Day, day.EndMinute
				last.worked += day.WorkedMinutes()
				last.breaks += day.BreakMinutes

				continue
			}
		}

		shifts = append(shifts, shift{
			startDay: day.Day, endDay: day.Day,
			startMinute: day.StartMinute, endMinute: day.EndMinute,
			worked: day.WorkedMinutes(), breaks: day.BreakMinutes,
		})
	}

	return shifts
}
