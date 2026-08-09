package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TimerRepository stores the one clock a user may have running.
//
// Keyed on the user rather than an id of its own, because there is at most one:
// Start replaces whatever was there, and Stop is Get followed by Clear.
type TimerRepository interface {
	// Get returns the running timer, or nil when nothing is running. Nothing
	// running is a normal state and not reported as an error.
	Get(ctx context.Context, userID uint) (*model.RunningTimer, error)

	// Start records a clock, replacing any the user already had.
	Start(ctx context.Context, timer *model.RunningTimer) error

	// Clear removes it, whether it was booked or discarded.
	Clear(ctx context.Context, userID uint) error

	// CountByProject is how many clocks are running against a project.
	//
	// For the deletion path: the column is a foreign key with no ON DELETE
	// behaviour, so a project removed while somebody is timing against it is a
	// constraint violation on PostgreSQL and MySQL, and on SQLite a clock that can
	// never be stopped.
	CountByProject(ctx context.Context, projectID uint) (int, error)
}
