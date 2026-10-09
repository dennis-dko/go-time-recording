package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// DirectoryRunRepository keeps what each directory synchronisation changed.
type DirectoryRunRepository interface {
	// Record keeps one run.
	Record(ctx context.Context, run *model.DirectoryRun) error

	// Latest returns the most recent runs, newest first, at most limit of them.
	Latest(ctx context.Context, limit int) ([]*model.DirectoryRun, error)
}
