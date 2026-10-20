package model

// VacationEntitlement is how many days of leave a year gives somebody, from a
// year on until the next entitlement's.
//
// From a year rather than one figure for good, for the reason a planned week is
// from a day: an allowance changes - more days with more years of service, fewer
// with part time - and the years before the change keep what they gave, so what
// they left over to carry stays what it was.
type VacationEntitlement struct {
	UserID   uint
	FromYear int
	Days     float64
}

// MaxEntitlementDays is the most days of leave a year can give: every day of it.
const MaxEntitlementDays = 366

// VacationDays is how much of the year's leave an absence uses: a day of leave
// one, half a day a half, and anything else none.
func (a Absence) VacationDays() float64 {
	switch a.Kind {
	case AbsenceVacation:
		return 1
	case AbsenceHalfVacation:
		return 0.5
	default:
		return 0
	}
}

// EntitlementIn returns the days of leave a year gives: the latest entitlement
// from that year or before. Before the first one there is none, which is what the
// second answer says.
func EntitlementIn(year int, entitlements []VacationEntitlement) (float64, bool) {
	days, found, from := 0.0, false, 0

	for _, entitlement := range entitlements {
		if entitlement.FromYear <= year && (!found || entitlement.FromYear > from) {
			days, found, from = entitlement.Days, true, entitlement.FromYear
		}
	}

	return days, found
}

// VacationYear is one year of somebody's leave: what the year gives, what the year
// before left over, and what has been taken of it and is still planned.
type VacationYear struct {
	Year        int
	Entitlement float64
	CarriedOver float64
	Taken       float64
	Planned     float64
}

// Remaining is what is left of the year's leave once everything planned is
// taken - negative where more was taken than there was.
func (y VacationYear) Remaining() float64 {
	return y.Entitlement + y.CarriedOver - y.Taken - y.Planned
}
