package model

import (
	"slices"
	"time"
)

// Absence is a day somebody does not work, and why - recorded by them, about
// themselves, as their hours are.
type Absence struct {
	UserID uint
	Day    time.Time
	Kind   string
}

const (
	// AbsenceVacation is a day of leave, which asks nothing.
	AbsenceVacation = "vacation"

	// AbsenceHalfVacation is half a day of leave, which asks half the day.
	AbsenceHalfVacation = "halfVacation"

	// AbsenceSick is a day off sick, which asks nothing.
	AbsenceSick = "sick"

	// AbsenceFlexDay is a day off taken from the overtime balance. It keeps what
	// the day asks, so the hours it does not book come off the balance - which is
	// what taking a day against overtime means.
	AbsenceFlexDay = "flexDay"

	// AbsenceLocalHoliday is a public holiday where somebody works that the state
	// does not keep as a whole, and which asks nothing.
	AbsenceLocalHoliday = "localHoliday"
)

// AbsenceUndisclosed is what an absence reads as to anybody but the person it is
// about - a manager they were assigned to: that the day was away, never why.
//
// Leave, a day off sick and a local holiday all ask the same nothing, and which of
// them it was is the owner's to tell. A day off sick in particular is a fact about
// somebody's health, which a manager has no business holding. Not a kind anybody
// records, so AbsenceKinds does not offer it and ValidAbsenceKind refuses it.
const AbsenceUndisclosed = "away"

// absenceKinds are the kinds, in the order a picker offers them.
var absenceKinds = []string{
	AbsenceVacation, AbsenceHalfVacation, AbsenceSick, AbsenceFlexDay, AbsenceLocalHoliday,
}

// AbsenceKinds returns the kinds a picker offers, as a copy nobody can change the
// original through.
func AbsenceKinds() []string {
	return slices.Clone(absenceKinds)
}

// ValidAbsenceKind reports whether a kind is one of the AbsenceKinds.
func ValidAbsenceKind(kind string) bool {
	return slices.Contains(absenceKinds, kind)
}

// Target is what a day planned at some hours asks with this absence on it.
func (a Absence) Target(planned float64) float64 {
	switch a.Kind {
	case AbsenceHalfVacation:
		return planned / 2
	case AbsenceFlexDay:
		return planned
	default:
		return 0
	}
}
