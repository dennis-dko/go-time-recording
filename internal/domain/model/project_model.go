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
	// empty: that was how a shared project was stored. Nothing creates one of those
	// any more and a migration gave every existing one an owner, so a nil here is
	// not "shared" - it is a row nobody can reach, which is what VisibleTo says
	// about it.
	OwnerID *uint
}

// VisibleTo reports whether the user may see the project, which is to say whether
// it is theirs.
func (p *Project) VisibleTo(userID uint) bool {
	return p.OwnerID != nil && *p.OwnerID == userID
}

// BelongsToSomebody reports whether the project has an owner at all.
//
// Only a leftover can fail this, and only until the migration that gave every
// project an owner has run. Worth being able to ask, so a check can say "nobody can
// see this" rather than quietly answering no to every reader.
func (p *Project) BelongsToSomebody() bool {
	return p.OwnerID != nil && *p.OwnerID != 0
}
