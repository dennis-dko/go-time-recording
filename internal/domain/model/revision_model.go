package model

import "time"

// TimesheetRevision is what an entry said before a correction replaced it: its day,
// its hours, its project and its description, and when it stopped saying so.
//
// Kept because the entry's own two moments - when it was booked and when it was
// last corrected - say that a figure moved and not what it moved from, and the
// person whose figure it is may need to know that when a month that added up
// yesterday does not today. Theirs alone to read, as the entry is.
type TimesheetRevision struct {
	ID          uint
	TimesheetID uint
	UserID      uint

	Date          time.Time
	DurationHours float64
	ProjectID     *uint
	Description   *string

	// ReplacedAt is when the correction that replaced this version was made.
	ReplacedAt time.Time

	// ReplacedBy is the manager who made that correction, nil where the entry's owner
	// made it.
	ReplacedBy *uint
}

// RevisionOf is what an entry says now, kept as the version a correction made at
// replacedAt replaced.
func RevisionOf(entry *Timesheet, replacedAt time.Time) TimesheetRevision {
	return TimesheetRevision{
		TimesheetID: entry.ID, UserID: entry.UserID,
		Date: entry.Date, DurationHours: entry.DurationHours,
		ProjectID: entry.ProjectID, Description: entry.Description,
		ReplacedAt: replacedAt,
	}
}

// Says reports whether two versions of an entry say the same: the same day, hours,
// project and description. A save that changes none of them corrects nothing, and
// keeps no revision.
func (t *Timesheet) Says(other *Timesheet) bool {
	sameProject := (t.ProjectID == nil) == (other.ProjectID == nil) &&
		(t.ProjectID == nil || *t.ProjectID == *other.ProjectID)

	sameDescription := (t.Description == nil) == (other.Description == nil) &&
		(t.Description == nil || *t.Description == *other.Description)

	return CalendarDay(t.Date).Equal(CalendarDay(other.Date)) && t.DurationHours == other.DurationHours &&
		t.UserID == other.UserID && sameProject && sameDescription
}
