package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// AdminEventRepository keeps the administration log.
type AdminEventRepository interface {
	// Record keeps a change, filling in its id.
	Record(ctx context.Context, event *model.AdminEvent) error

	// Before reads up to limit changes older than the one with the given id, the
	// newest first; an id of zero reads from the newest.
	Before(ctx context.Context, beforeID uint, limit int) ([]model.AdminEvent, error)
}
