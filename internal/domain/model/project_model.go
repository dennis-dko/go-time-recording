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
}
