package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// TemplateRepository keeps the bookings people save to fill the form with.
type TemplateRepository interface {
	// ForUser returns somebody's templates, in the order they were saved.
	ForUser(ctx context.Context, userID uint) ([]model.EntryTemplate, error)

	// Save adds a template and returns it with its id.
	Save(ctx context.Context, template model.EntryTemplate) (*model.EntryTemplate, error)

	// Delete removes one template, scoped to its owner so nobody can remove
	// anybody else's, and reports whether there was one to remove.
	Delete(ctx context.Context, id, userID uint) (bool, error)
}
