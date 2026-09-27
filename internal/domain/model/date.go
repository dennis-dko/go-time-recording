package model

import "time"

// CalendarDay is the day an instant falls on, expressed the way every stored
// date in this application is: midnight UTC.
//
// A date here answers "which day", and that question has no time of day in it
// and no zone either - 3 July is 3 July whether it is read in Berlin or in Los
// Angeles. A date posted as "2026-07-03" parses to midnight UTC and is stored
// that way, so anything built from a clock has to arrive in the same shape or
// the same field holds two different things.
//
// The calendar fields are read in the instant's own location, which is the
// point: midnight in Berlin is still the third there, and stays the third here.
func CalendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// OnTheCalendar reports whether a day is one this application reads and shows:
// from 1 January 1900 to 31 December 9999.
//
// The range a spreadsheet can hold, and so the range the importer reads, which
// every other way in has to agree with. A date field is a few keystrokes from a
// day in the year 202, and a booking there was stored on every dialect and then
// shown by no range anybody looks at - lost as surely as if it had been deleted,
// and harder to find.
func OnTheCalendar(day time.Time) bool {
	return !day.Before(firstCalendarDay) && day.Before(afterLastCalendarDay)
}

var (
	// firstCalendarDay and afterLastCalendarDay bound OnTheCalendar.
	firstCalendarDay     = time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	afterLastCalendarDay = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
)
