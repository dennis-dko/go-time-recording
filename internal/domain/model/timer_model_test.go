package model_test

import (
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A clock measures what it measures. The two things worth pinning are that the
// duration is not rounded to anything, and which calendar day the finished entry
// lands on - which is the question a stopwatch running over midnight asks and a
// typed booking never does.

func TestElapsedIsNotRoundedToAnything(t *testing.T) {
	start := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	timer := model.RunningTimer{StartedAt: start}

	cases := map[time.Duration]float64{
		19 * time.Minute:                            19.0 / 60,
		time.Hour + 23*time.Minute:                  1 + 23.0/60,
		37 * time.Second:                            37.0 / 3600,
		2*time.Hour + 7*time.Minute + 3*time.Second: 2 + 7.0/60 + 3.0/3600,
	}

	for elapsed, want := range cases {
		got := timer.HoursElapsed(start.Add(elapsed))

		// Exact to within floating-point noise, not to within a quarter of an
		// hour: whoever pressed stop after nineteen minutes worked nineteen
		// minutes.
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%v elapsed came out as %v hours, want %v", elapsed, got, want)
		}
	}
}

// The day the clock was started, in the user's own zone - not the day it was
// stopped, and not the day it would be in UTC.
//
// Somebody who starts at half past eleven at night and stops after midnight worked
// that evening, and an entry on the following day would disagree with both their
// memory and their week.
func TestTheBookingDayIsTheDayItStartedInTheUsersZone(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("cannot load the zone: %v", err)
	}

	// 23:30 in Berlin on the 5th is 21:30 UTC on the 5th - same day either way,
	// so this one is about the zone being applied at all.
	evening := model.RunningTimer{
		StartedAt: time.Date(2026, 8, 5, 21, 30, 0, 0, time.UTC),
	}

	if day := evening.BookingDay(berlin); day.Format("2006-01-02") != "2026-08-05" {
		t.Errorf("the entry lands on %s, want 2026-08-05", day.Format("2006-01-02"))
	}

	// 00:30 in Berlin on the 6th is 22:30 UTC on the 5th. The zone is what makes
	// these different days, and the user's answer is the 6th.
	afterMidnight := model.RunningTimer{
		StartedAt: time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC),
	}

	if day := afterMidnight.BookingDay(berlin); day.Format("2006-01-02") != "2026-08-06" {
		t.Errorf("the entry lands on %s, want 2026-08-06 in Berlin", day.Format("2006-01-02"))
	}

	// And in UTC the same instant is still the 5th, which is the disagreement the
	// zone exists to settle.
	if day := afterMidnight.BookingDay(time.UTC); day.Format("2006-01-02") != "2026-08-05" {
		t.Errorf("in UTC the entry lands on %s, want 2026-08-05", day.Format("2006-01-02"))
	}

	// A clock that runs past midnight books on the evening it started, whatever
	// time it is stopped.
	overMidnight := model.RunningTimer{
		StartedAt: time.Date(2026, 8, 5, 21, 30, 0, 0, time.UTC),
	}

	stopped := overMidnight.StartedAt.Add(3 * time.Hour) // 00:30 Berlin, next day
	if day := overMidnight.BookingDay(berlin); day.Format("2006-01-02") != "2026-08-05" {
		t.Errorf("a clock stopped at %v booked on %s, want the evening it started",
			stopped.In(berlin).Format("15:04"), day.Format("2006-01-02"))
	}
}

// A nil zone must not panic. It happens when nothing has been configured
// anywhere, and UTC is what the rest of the application falls back to.
func TestANilZoneFallsBackToUTC(t *testing.T) {
	timer := model.RunningTimer{
		StartedAt: time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC),
	}

	if day := timer.BookingDay(nil); day.Format("2006-01-02") != "2026-08-05" {
		t.Errorf("a nil zone gave %s, want the UTC day", day.Format("2006-01-02"))
	}
}

// The two clocks that cannot become an entry: one nobody meant to start, and one
// somebody forgot to stop.
func TestATimerTooShortOrTooLongToBook(t *testing.T) {
	start := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	timer := model.RunningTimer{StartedAt: start}

	// Half a minute is below the smallest bookable duration, which is what an
	// accidental double-press produces.
	if !timer.TimerTooShort(start.Add(20 * time.Second)) {
		t.Error("20 seconds is not reported as too short to book")
	}

	if timer.TimerTooShort(start.Add(time.Minute)) {
		t.Error("a minute is reported as too short, but it is bookable")
	}

	// More than a day is a clock somebody forgot, and one entry cannot hold it.
	if !timer.TimerTooLong(start.Add(25 * time.Hour)) {
		t.Error("25 hours is not reported as too long for one entry")
	}

	if timer.TimerTooLong(start.Add(8 * time.Hour)) {
		t.Error("a working day is reported as too long")
	}
}
