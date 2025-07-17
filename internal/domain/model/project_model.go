package model

import "time"

const (
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
