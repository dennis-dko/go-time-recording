package model_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// dayNames writes holidays as their day and name, for comparing with a list read
// off a calendar.
func dayNames(holidays []model.Holiday) []string {
	written := make([]string, 0, len(holidays))
	for _, holiday := range holidays {
		written = append(written, holiday.Day.Format(time.DateOnly)+" "+holiday.Name)
	}

	return written
}

// Easter, through the one state that keeps Easter Sunday itself as a holiday,
// against the dates in a calendar.
func TestEasterFallsOnItsDay(t *testing.T) {
	for year, want := range map[int]string{
		2000: "2000-04-23", 2019: "2019-04-21", 2024: "2024-03-31", 2025: "2025-04-20",
		2026: "2026-04-05", 2027: "2027-03-28", 2038: "2038-04-25",
	} {
		found := slices.IndexFunc(model.HolidaysIn("BB", year),
			func(h model.Holiday) bool { return h.Name == "easterSunday" })

		if found < 0 {
			t.Errorf("%d has no Easter Sunday in Brandenburg", year)

			continue
		}

		if got := model.HolidaysIn("BB", year)[found].Day.Format(time.DateOnly); got != want {
			t.Errorf("Easter %d fell on %s, want %s", year, got, want)
		}
	}
}

// A whole year of two states, as their calendars list it.
func TestAStatesYearIsItsCalendars(t *testing.T) {
	for _, tc := range []struct {
		region string
		year   int
		want   []string
	}{
		{"BY", 2026, []string{
			"2026-01-01 newYear", "2026-01-06 epiphany", "2026-04-03 goodFriday",
			"2026-04-06 easterMonday", "2026-05-01 labourDay", "2026-05-14 ascension",
			"2026-05-25 whitMonday", "2026-06-04 corpusChristi", "2026-10-03 germanUnity",
			"2026-11-01 allSaints", "2026-12-25 christmasDay", "2026-12-26 stStephensDay",
		}},
		{"BE", 2025, []string{
			"2025-01-01 newYear", "2025-03-08 womensDay", "2025-04-18 goodFriday",
			"2025-04-21 easterMonday", "2025-05-01 labourDay", "2025-05-08 liberationDay",
			"2025-05-29 ascension", "2025-06-09 whitMonday", "2025-10-03 germanUnity",
			"2025-12-25 christmasDay", "2025-12-26 stStephensDay",
		}},
	} {
		if got := dayNames(model.HolidaysIn(tc.region, tc.year)); !slices.Equal(got, tc.want) {
			t.Errorf("%s %d reads\n%v\nwant\n%v", tc.region, tc.year, got, tc.want)
		}
	}
}

// A holiday holds from the year it was introduced, until the year it was
// abolished, and once in a year it was given once - and a state that keeps it
// anyway does not get it twice.
func TestAHolidayHoldsInItsYearsOnly(t *testing.T) {
	has := func(region string, year int, name string) int {
		count := 0

		for _, holiday := range model.HolidaysIn(region, year) {
			if holiday.Name == name {
				count++
			}
		}

		return count
	}

	for _, tc := range []struct {
		region string
		year   int
		name   string
		want   int
	}{
		{"NI", 2016, "reformationDay", 0},
		{"NI", 2017, "reformationDay", 1},
		{"NI", 2018, "reformationDay", 1},
		{"BW", 2017, "reformationDay", 1},
		{"BW", 2018, "reformationDay", 0},
		{"SN", 2017, "reformationDay", 1},
		{"BE", 2018, "womensDay", 0},
		{"BE", 2019, "womensDay", 1},
		{"BE", 2024, "liberationDay", 0},
		{"NW", 1994, "repentanceDay", 1},
		{"NW", 1995, "repentanceDay", 0},
		{"SN", 2026, "repentanceDay", 1},
	} {
		if got := has(tc.region, tc.year, tc.name); got != tc.want {
			t.Errorf("%s %d has %s %d time(s), want %d", tc.region, tc.year, tc.name, got, tc.want)
		}
	}

	// The Wednesday before the 23rd of November.
	for _, holiday := range model.HolidaysIn("SN", 2026) {
		if holiday.Name == "repentanceDay" && holiday.Day.Format(time.DateOnly) != "2026-11-18" {
			t.Errorf("the Day of Repentance 2026 fell on %s, want 2026-11-18", holiday.Day.Format(time.DateOnly))
		}
	}
}

// No region has no holidays, and one this application does not know is refused
// rather than answered with none.
func TestNoRegionHasNoHolidays(t *testing.T) {
	if got := model.HolidaysIn("", 2026); len(got) != 0 {
		t.Errorf("no region has %v", dayNames(got))
	}

	if model.ValidHolidayRegion("XX") || !model.ValidHolidayRegion("") || !model.ValidHolidayRegion("HH") {
		t.Error("the regions accepted are not the sixteen states and none")
	}

	if got := model.HolidaysIn("XX", 2026); len(got) != 0 {
		t.Errorf("an unknown region has %v", dayNames(got))
	}
}

// A range takes its holidays from every year it touches, and only from inside it.
func TestHolidaysBetweenTwoDaysSpanTheYearsBetween(t *testing.T) {
	found := model.HolidaysBetween("BY",
		time.Date(2025, time.December, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.January, 6, 0, 0, 0, 0, time.UTC))

	var days []string
	for day := range found {
		days = append(days, day.Format(time.DateOnly))
	}

	slices.Sort(days)

	if want := []string{"2025-12-25", "2025-12-26", "2026-01-01", "2026-01-06"}; !slices.Equal(days, want) {
		t.Errorf("the turn of the year in Bavaria has %v, want %v", days, want)
	}
}

// Every region a picker offers is one the rules know, with at least the nine
// holidays every state keeps.
func TestEveryRegionOfferedHasItsHolidays(t *testing.T) {
	regions := model.HolidayRegions()

	if len(regions) != 16 {
		t.Fatalf("%d regions are offered, want the sixteen states", len(regions))
	}

	for _, region := range regions {
		if !model.ValidHolidayRegion(region) {
			t.Errorf("%s is offered and then refused", region)
		}

		if got := len(model.HolidaysIn(region, 2026)); got < 9 {
			t.Errorf("%s has %d holidays in 2026, want at least the nine every state keeps", region, got)
		}
	}

	regions[0] = "XX"

	if model.HolidayRegions()[0] == "XX" {
		t.Error("changing the list handed out changed the regions themselves")
	}
}
