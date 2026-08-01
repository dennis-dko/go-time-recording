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
type Project struct {
	ID          uint
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string

	// OwnerID makes the project private to one user, where it acts as a
	// personal category for splitting up a day. nil is a shared project,
	// visible to everyone who may read projects.
	OwnerID *uint
}

// IsPrivate reports whether the project belongs to a single user.
func (p *Project) IsPrivate() bool {
	return p.OwnerID != nil && *p.OwnerID != 0
}

// VisibleTo reports whether the user may see the project: shared projects are
// visible to everyone, private ones only to their owner.
func (p *Project) VisibleTo(userID uint) bool {
	return !p.IsPrivate() || *p.OwnerID == userID
}
