package model

import "time"

const (
	ProjectStatusArchived  = "archived"
	ProjectStatusCompleted = "completed"
)

// Project model
type Project struct {
	ID          uint       `json:"id"  sql:"auto_increment"`
	Name        string     `json:"name" sql:"not_null"`
	Description *string    `json:"description,omitempty"`
	StartDate   time.Time  `json:"startDate" sql:"not_null"`
	EndDate     *time.Time `json:"endDate,omitempty"`
	Status      string     `json:"status" sql:"not_null"`
}

func (p *Project) TableName() string {
	return "project"
}
