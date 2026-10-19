package model

import (
	"slices"
	"strings"
	"time"
)

// Holiday is a public holiday: its day, and which one it is, by a name the
// interface translates.
type Holiday struct {
	Day  time.Time
	Name string
}

// holidayRegions are the German states whose public holidays are worked out here,
// by their ISO 3166-2 code without the country, in the order a picker offers them.
// No region at all is the empty string, and has no holidays.
//
// Worked out rather than looked up: every one of them is a fixed date or a fixed
// distance from Easter, so a calendar service would be a dependency, and a network
// call under every balance, for something the binary can compute.
var holidayRegions = []string{
	"BW", "BY", "BE", "BB", "HB", "HH", "HE", "MV",
	"NI", "NW", "RP", "SL", "SN", "ST", "SH", "TH",
}

// HolidayRegions returns the regions a picker offers, as a copy nobody can change
// the original through.
func HolidayRegions() []string {
	return slices.Clone(holidayRegions)
}

// ValidHolidayRegion reports whether a region is one of the HolidayRegions, or none.
func ValidHolidayRegion(region string) bool {
	return region == "" || slices.Contains(holidayRegions, region)
}

// holidayRule is one holiday and where and when it holds.
type holidayRule struct {
	name string
	on   func(year int) time.Time

	// regions it holds in, separated by spaces; empty is every state.
	regions string

	// from and until bound the years it holds in, zero leaving that end open.
	from, until int

	// only, when set, are the one-off years it holds in and no others.
	only []int
}

// holidayRules are the public holidays that hold in the whole of a state.
//
// Not those that hold in part of one - the Assumption in Bavaria's Catholic
// municipalities, Corpus Christi in parts of Saxony and Thuringia, the Peace
// Festival in Augsburg - because a state's answer cannot know which part somebody
// works in, and granting such a day to the whole state is as wrong as granting it
// to nobody. Somebody it applies to records it as an absence, AbsenceLocalHoliday.
//
// A holiday a state introduced counts from the year it was introduced, and one
// that was abolished until the year it was, so working out an earlier year does
// not rewrite what that year asked.
var holidayRules = []holidayRule{
	{name: "newYear", on: fixed(time.January, 1)},
	{name: "epiphany", on: fixed(time.January, 6), regions: "BW BY ST"},
	{name: "womensDay", on: fixed(time.March, 8), regions: "BE", from: 2019},
	{name: "womensDay", on: fixed(time.March, 8), regions: "MV", from: 2023},
	{name: "goodFriday", on: afterEaster(-2)},
	{name: "easterSunday", on: afterEaster(0), regions: "BB"},
	{name: "easterMonday", on: afterEaster(1)},
	{name: "labourDay", on: fixed(time.May, 1)},
	{name: "liberationDay", on: fixed(time.May, 8), regions: "BE", only: []int{2020, 2025}},
	{name: "ascension", on: afterEaster(39)},
	{name: "whitSunday", on: afterEaster(49), regions: "BB"},
	{name: "whitMonday", on: afterEaster(50)},
	{name: "corpusChristi", on: afterEaster(60), regions: "BW BY HE NW RP SL"},
	{name: "assumption", on: fixed(time.August, 15), regions: "SL"},
	{name: "childrensDay", on: fixed(time.September, 20), regions: "TH", from: 2019},
	{name: "germanUnity", on: fixed(time.October, 3), from: 1990},
	{name: "reformationDay", on: fixed(time.October, 31), regions: "BB MV SN ST TH"},
	{name: "reformationDay", on: fixed(time.October, 31), regions: "HB HH NI SH", from: 2018},
	{name: "reformationDay", on: fixed(time.October, 31), only: []int{2017}},
	{name: "allSaints", on: fixed(time.November, 1), regions: "BW BY NW RP SL"},
	{name: "repentanceDay", on: repentanceDay, regions: "SN"},
	{name: "repentanceDay", on: repentanceDay, until: 1994},
	{name: "christmasDay", on: fixed(time.December, 25)},
	{name: "stStephensDay", on: fixed(time.December, 26)},
}

// HolidaysIn returns the public holidays of one year in one region, earliest
// first. No region has none.
func HolidaysIn(region string, year int) []Holiday {
	if region == "" || !ValidHolidayRegion(region) {
		return nil
	}

	var holidays []Holiday

	for _, rule := range holidayRules {
		if !rule.holdsIn(region, year) {
			continue
		}

		day := rule.on(year)

		// Twice where two rules give the same day - the Reformation's 2017 in a
		// state that keeps it every year - and once is what it is.
		if slices.ContainsFunc(holidays, func(h Holiday) bool { return h.Day.Equal(day) }) {
			continue
		}

		holidays = append(holidays, Holiday{Day: day, Name: rule.name})
	}

	slices.SortFunc(holidays, func(a, b Holiday) int { return a.Day.Compare(b.Day) })

	return holidays
}

// HolidaysBetween returns the public holidays of a region from one day to another,
// both included, keyed by their day.
func HolidaysBetween(region string, from, to time.Time) map[time.Time]Holiday {
	from, to = CalendarDay(from), CalendarDay(to)
	found := map[time.Time]Holiday{}

	for year := from.Year(); year <= to.Year(); year++ {
		for _, holiday := range HolidaysIn(region, year) {
			if !holiday.Day.Before(from) && !holiday.Day.After(to) {
				found[holiday.Day] = holiday
			}
		}
	}

	return found
}

func (r holidayRule) holdsIn(region string, year int) bool {
	if r.regions != "" && !slices.Contains(strings.Fields(r.regions), region) {
		return false
	}

	if r.only != nil {
		return slices.Contains(r.only, year)
	}

	return (r.from == 0 || year >= r.from) && (r.until == 0 || year <= r.until)
}

// fixed is a holiday on the same date every year.
func fixed(month time.Month, day int) func(int) time.Time {
	return func(year int) time.Time { return time.Date(year, month, day, 0, 0, 0, 0, time.UTC) }
}

// afterEaster is a holiday a number of days from Easter Sunday.
func afterEaster(days int) func(int) time.Time {
	return func(year int) time.Time { return easterSunday(year).AddDate(0, 0, days) }
}

// repentanceDay is the Wednesday before the 23rd of November.
func repentanceDay(year int) time.Time {
	day := time.Date(year, time.November, 22, 0, 0, 0, 0, time.UTC)

	for day.Weekday() != time.Wednesday {
		day = day.AddDate(0, 0, -1)
	}

	return day
}

// easterSunday is the Western Easter of a year, by the Gregorian computus in the
// form Meeus gives it - which needs nothing but integer arithmetic.
func easterSunday(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1

	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}
