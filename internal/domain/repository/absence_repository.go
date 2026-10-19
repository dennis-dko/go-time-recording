package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// AbsenceRepository keeps the days people record themselves as away.
type AbsenceRepository interface {
	// Between returns somebody's absences from one day to another, both included,
	// earliest first.
	Between(ctx context.Context, userID uint, from, to time.Time) ([]model.Absence, error)

	// Save records absences, each replacing whatever was recorded on its day, and
	// writes all of them or none.
	Save(ctx context.Context, absences []model.Absence) error

	// Delete removes the absence recorded on a day, and reports whether there was
	// one to remove.
	Delete(ctx context.Context, userID uint, day time.Time) (bool, error)
}
