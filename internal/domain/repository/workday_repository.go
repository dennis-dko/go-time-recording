package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// WorkDayRepository keeps when each person worked, a day at a time.
type WorkDayRepository interface {
	// Between returns somebody's recorded days from one day to another, both
	// included, earliest first.
	Between(ctx context.Context, userID uint, from, to time.Time) ([]model.WorkDay, error)

	// Save records a day, replacing what was recorded for it.
	Save(ctx context.Context, day model.WorkDay) error

	// Delete removes a recorded day, and reports whether there was one.
	Delete(ctx context.Context, userID uint, day time.Time) (bool, error)
}
