package model

import "time"

const (
	// ProjectStatusActive is the state a project starts in: it accepts new
	// time entries.
	ProjectStatusActive    = "active"
	ProjectStatusArchived  = "archived"
	ProjectStatusCompleted = "completed"
)

// Project model
//
// Every project belongs to one person, and to nobody else. There used to be two
// kinds - a shared project everyone could see and book on, and a private category
// for splitting up your own day - and the second was an exception to the first.
// Now the exception is the whole rule: each account is its own world, so a project
// is one person's way of organising their own hours.
//
// Two people working on the same thing therefore have a project each, with the same
// name if they like. Nothing adds them together, because nothing is meant to: there
// are no teams here and no view across accounts.
type Project struct {
	ID          uint
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string

	// OwnerID is whose project it is.
	//
	// A pointer because the column is nullable and was, for a while, genuinely
	// empty: that was how a shared project was stored. A migration gave every
	// existing one an owner, so a nil here is not "shared" - it is a row nobody
	// can reach, which is what VisibleTo says about it.
	//
	// One path still produces one: with AUTH_ENABLED=false there is no "whose" to
	// record, so a project created then has no owner. That is consistent while
	// enforcement is off, because RequireVisible short-circuits on a viewer id of
	// zero and sees everything. It stops being consistent the moment somebody
	// turns authentication on - those rows are then invisible to every account,
	// including the one that made them, and there is no screen that can reach them
	// to fix it. deploy/OPERATIONS.md says so under "Things that will surprise
	// you"; it is the reason that mode is documented as a throwaway trial.
	OwnerID *uint
}

// VisibleTo reports whether the user may see the project, which is to say whether
// it is theirs.
func (p *Project) VisibleTo(userID uint) bool {
	return p.OwnerID != nil && *p.OwnerID == userID
}
