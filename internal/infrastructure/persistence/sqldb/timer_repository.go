package sqldb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// TimerRepository stores the one clock a user may have running.
type TimerRepository struct {
	base
}

// NewTimerRepository creates a timer repository for the given dialect.
func NewTimerRepository(db DB, dialect string) *TimerRepository {
	return &TimerRepository{base{db: db, dialect: dialect}}
}

var _ repository.TimerRepository = (*TimerRepository)(nil)

// Get returns the running timer, or nil when nothing is running.
func (r *TimerRepository) Get(ctx context.Context, userID uint) (*model.RunningTimer, error) {
	timer := model.RunningTimer{UserID: userID}

	var projectID sql.NullInt64

	err := r.db.QueryRowContext(ctx, r.rebind(
		"SELECT project_id, description, started_at FROM running_timers WHERE user_id = ?"),
		userID).Scan(&projectID, &timer.Description, &timer.StartedAt)

	// Nothing running is the ordinary state, not a failure.
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	if projectID.Valid {
		id := uint(projectID.Int64)
		timer.ProjectID = &id
	}

	return &timer, nil
}

// Start records a clock, replacing any the user already had.
//
// Delete-then-insert rather than a dialect-specific upsert, for the same reason
// the settings repository does an update-then-insert: ON CONFLICT and ON DUPLICATE
// KEY are spelled differently by each engine. Both statements go in one
// transaction, so a connection lost between them cannot leave the user with no
// clock at all when they asked for a new one.
func (r *TimerRepository) Start(ctx context.Context, timer *model.RunningTimer) error {
	return r.withTx(ctx, func(tx base) error {
		if _, err := tx.exec(ctx,
			"DELETE FROM running_timers WHERE user_id = ?", timer.UserID); err != nil {
			return apperror.Internal(err)
		}

		var projectID any
		if timer.ProjectID != nil {
			projectID = *timer.ProjectID
		}

		_, err := tx.exec(ctx,
			"INSERT INTO running_timers (user_id, project_id, description, started_at) "+
				"VALUES (?, ?, ?, ?)",
			timer.UserID, projectID, timer.Description, timer.StartedAt)
		if err != nil {
			return apperror.Internal(err)
		}

		return nil
	})
}

// Clear removes it, whether it was booked or discarded.
func (r *TimerRepository) Clear(ctx context.Context, userID uint) error {
	if _, err := r.exec(ctx, "DELETE FROM running_timers WHERE user_id = ?", userID); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

// CountByProject is how many running clocks point at a project.
func (r *TimerRepository) CountByProject(ctx context.Context, projectID uint) (int, error) {
	var count int

	err := r.db.QueryRowContext(ctx,
		r.rebind("SELECT COUNT(*) FROM running_timers WHERE project_id = ?"),
		projectID).Scan(&count)
	if err != nil {
		return 0, apperror.Internal(err)
	}

	return count, nil
}
