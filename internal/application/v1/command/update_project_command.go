package command

import (
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
)

// UpdateProjectCommand command to update existing project
type UpdateProjectCommand struct {
	ID          uint
	Name        *string
	Description *string
	StartDate   *time.Time
	EndDate     *time.Time
	Status      *string

	// ClearEndDate takes the end date off, which no pointer above can say.
	//
	// A nil EndDate means "leave it alone", which is what a partial update needs
	// and what every caller that says nothing about the date relies on - the
	// spreadsheet import among them. That leaves no way to say "there is no end
	// any more", and an empty date does not stand in for one: a zero time is
	// before every start date there is, so it comes back as an invalid endDate
	// rather than as a project that is open again.
	//
	// Its own field rather than a second meaning for the pointer, so the two
	// cannot be confused with each other or with silence.
	ClearEndDate bool

	// ActorID is who is editing. A private project may only be changed by its
	// owner; zero skips the check.
	ActorID uint
}

// UpdateProjectCommandResult command to get update result of existing project
type UpdateProjectCommandResult struct {
	Result *common.ProjectResult
}
