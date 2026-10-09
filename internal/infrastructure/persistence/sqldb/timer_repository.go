package sqldb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
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

	err := r.db.QueryRowContext(ctx, r.rebind(
		"SELECT project_id, description, started_at FROM running_timers WHERE user_id = ?"),
		userID).Scan(&timer.ProjectID, &timer.Description, &timer.StartedAt)

	// Nothing running is the ordinary state, not a failure.
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return &timer, nil
}

// Start records a clock, replacing any the user already had.
//
// One statement that replaces the row, in each engine's own words. It was a
// delete followed by an insert, which is right one request at a time and wrong
// for two at once: on PostgreSQL both deletes found nothing, both inserts reached
// the primary key, and the second press answered 500 with a duplicate-key error
// for asking for what the first one got. A start replaces the running clock, so
// either order is a correct answer, and the database is left to pick one.
//
// Spelled as the settings repository spells its upsert, since no form is
// portable: ON CONFLICT on SQLite and PostgreSQL, ON DUPLICATE KEY on MySQL.
func (r *TimerRepository) Start(ctx context.Context, timer *model.RunningTimer) error {
	query := `INSERT INTO running_timers (user_id, project_id, description, started_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET project_id = EXCLUDED.project_id,
			description = EXCLUDED.description, started_at = EXCLUDED.started_at`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO running_timers (user_id, project_id, description, started_at)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE project_id = VALUES(project_id),
				description = VALUES(description), started_at = VALUES(started_at)`
	}

	if _, err := r.exec(ctx, query,
		timer.UserID, timer.ProjectID, timer.Description, timer.StartedAt); err != nil {
		return apperror.Internal(err)
	}

	return nil
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
