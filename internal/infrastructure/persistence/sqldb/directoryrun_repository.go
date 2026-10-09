package sqldb

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// DirectoryRunRepository keeps what each directory synchronisation changed.
type DirectoryRunRepository struct {
	base
}

// NewDirectoryRunRepository reads and writes the runs of the given database.
func NewDirectoryRunRepository(db DB, dialect string) *DirectoryRunRepository {
	return &DirectoryRunRepository{base{db: db, dialect: dialect}}
}

var _ repository.DirectoryRunRepository = (*DirectoryRunRepository)(nil)

// Record keeps one run.
func (r *DirectoryRunRepository) Record(ctx context.Context, run *model.DirectoryRun) error {
	id, err := r.insert(ctx,
		"INSERT INTO directory_runs (ran_at, confirmed, deleted, entries_deleted, created) "+
			"VALUES (?, ?, ?, ?, ?)",
		run.RanAt, run.Confirmed, run.Deleted, run.EntriesDeleted, run.Created)
	if err != nil {
		return apperror.Internal(err)
	}

	run.ID = id

	return nil
}

// Latest returns the most recent runs, newest first.
func (r *DirectoryRunRepository) Latest(ctx context.Context, limit int) ([]*model.DirectoryRun, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT id, ran_at, confirmed, deleted, entries_deleted, created "+
			"FROM directory_runs ORDER BY ran_at DESC, id DESC LIMIT ?"), limit)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	runs := make([]*model.DirectoryRun, 0)

	for rows.Next() {
		var (
			run   model.DirectoryRun
			ranAt dateTime
		)

		if err := rows.Scan(&run.ID, &ranAt, &run.Confirmed, &run.Deleted, &run.EntriesDeleted,
			&run.Created); err != nil {
			return nil, apperror.Internal(err)
		}

		run.RanAt = ranAt.Time
		runs = append(runs, &run)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return runs, nil
}
