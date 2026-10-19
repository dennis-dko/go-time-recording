package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// VacationRepository keeps how many days of leave a year gives each person.
type VacationRepository interface {
	// ForUser returns somebody's entitlements, earliest first.
	ForUser(ctx context.Context, userID uint) ([]model.VacationEntitlement, error)

	// Save records an entitlement, replacing one from the same year.
	Save(ctx context.Context, entitlement model.VacationEntitlement) error

	// Delete removes the entitlement from a year, and reports whether there was
	// one to remove.
	Delete(ctx context.Context, userID uint, fromYear int) (bool, error)
}
