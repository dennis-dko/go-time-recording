package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// WorkScheduleRepository keeps the weeks people plan their hours in.
//
// A person has any number of them, one per day a new week began; the one in
// force on a day is the latest that started on or before it. Keyed on the person
// and that first day, so two plans can never claim the same day.
type WorkScheduleRepository interface {
	// ForUser returns somebody's plans, earliest first - the order TargetOn needs.
	ForUser(ctx context.Context, userID uint) ([]model.WorkSchedule, error)

	// Save records a plan, replacing the one that starts on the same day.
	Save(ctx context.Context, schedule model.WorkSchedule) error

	// Delete removes the plan that starts on a day, and reports whether there was
	// one.
	Delete(ctx context.Context, userID uint, validFrom time.Time) (bool, error)
}
