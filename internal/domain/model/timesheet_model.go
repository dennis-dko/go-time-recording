package model

import "time"

// Timesheet model
//
// There is no status. An entry used to travel open -> submitted -> approved, with
// an approved one locked against further change - which needs somebody to do the
// approving. There is nobody: everyone keeps their own hours, and the built-in
// administrator runs the installation rather than reviewing other people's work.
// A review path with no reviewer is a step that only ever gets in the way of the
// person who recorded the time, so it is gone, along with the column that carried
// it.
type Timesheet struct {
	ID     uint
	UserID uint

	// ProjectID is optional: hours can be recorded first and categorised
	// later, or left uncategorised entirely.
	ProjectID *uint

	Date          time.Time
	DurationHours float64
	Description   *string

	// CreatedAt is when the entry was recorded and UpdatedAt when it was last
	// changed. Neither is Date, which is the day the work was done - the one field
	// a correction leaves alone, and therefore the one that cannot say whether the
	// figure has moved since somebody first wrote it down.
	//
	// Zero means unknown rather than 1970: an entry booked before the columns
	// existed has no recorded moment, and nothing invents one for it. Both are
	// written by the repository, so every write path gets them without each caller
	// remembering to.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasProject reports whether the entry is assigned to a project.
func (t *Timesheet) HasProject() bool {
	return t.ProjectID != nil && *t.ProjectID != 0
}
