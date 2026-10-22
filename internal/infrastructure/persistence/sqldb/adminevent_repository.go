package sqldb

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// AdminEventRepository keeps the administration log.
type AdminEventRepository struct {
	base
}

// NewAdminEventRepository reads and writes the log of the given database.
func NewAdminEventRepository(db DB, dialect string) *AdminEventRepository {
	return &AdminEventRepository{base{db: db, dialect: dialect}}
}

var _ repository.AdminEventRepository = (*AdminEventRepository)(nil)

// Record keeps one change.
func (r *AdminEventRepository) Record(ctx context.Context, event *model.AdminEvent) error {
	id, err := r.insert(ctx,
		"INSERT INTO admin_events (at, actor_id, action, target_id, detail) VALUES (?, ?, ?, ?, ?)",
		event.At, event.ActorID, event.Action, nullableID(event.TargetID), event.Detail)
	if err != nil {
		return apperror.Internal(err)
	}

	event.ID = id

	return nil
}

// Before reads a page of the log, newest first, by id: an entry's id grows with
// the moment it was written, and a page asked for by the last id it showed cannot
// skip or repeat an entry the way one asked for by a moment can.
func (r *AdminEventRepository) Before(ctx context.Context, beforeID uint, limit int) ([]model.AdminEvent, error) {
	query := "SELECT id, at, actor_id, action, target_id, detail FROM admin_events ORDER BY id DESC LIMIT ?"
	args := []any{limit}

	if beforeID > 0 {
		query = "SELECT id, at, actor_id, action, target_id, detail FROM admin_events WHERE id < ? " +
			"ORDER BY id DESC LIMIT ?"
		args = []any{beforeID, limit}
	}

	rows, err := r.db.QueryContext(ctx, r.rebind(query), args...)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	events := make([]model.AdminEvent, 0)

	for rows.Next() {
		var (
			event  model.AdminEvent
			at     dateTime
			target *uint
		)

		if err := rows.Scan(&event.ID, &at, &event.ActorID, &event.Action, &target, &event.Detail); err != nil {
			return nil, apperror.Internal(err)
		}

		event.At = at.Time

		if target != nil {
			event.TargetID = *target
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return events, nil
}

// nullableID is an id for a column that may name nothing: NULL for zero, which is
// what a change to the installation as a whole has as its target.
func nullableID(id uint) any {
	if id == 0 {
		return nil
	}

	return id
}
