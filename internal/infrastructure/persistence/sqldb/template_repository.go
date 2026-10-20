package sqldb

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// TemplateRepository keeps the bookings people save to fill the form with.
type TemplateRepository struct {
	base
}

// NewTemplateRepository creates a template repository for the given dialect.
func NewTemplateRepository(db DB, dialect string) *TemplateRepository {
	return &TemplateRepository{base{db: db, dialect: dialect}}
}

var _ repository.TemplateRepository = (*TemplateRepository)(nil)

// ForUser returns somebody's templates, in the order they were saved.
func (r *TemplateRepository) ForUser(ctx context.Context, userID uint) ([]model.EntryTemplate, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT id, project_id, duration_hours, description FROM entry_templates WHERE user_id = ? ORDER BY id"),
		userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var templates []model.EntryTemplate

	for rows.Next() {
		template := model.EntryTemplate{UserID: userID}

		if err := rows.Scan(&template.ID, &template.ProjectID,
			&template.DurationHours, &template.Description); err != nil {
			return nil, apperror.Internal(err)
		}

		templates = append(templates, template)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return templates, nil
}

// Save adds a template and returns it with its id.
func (r *TemplateRepository) Save(ctx context.Context, template model.EntryTemplate) (*model.EntryTemplate, error) {
	id, err := r.insert(ctx,
		"INSERT INTO entry_templates (user_id, project_id, duration_hours, description) VALUES (?, ?, ?, ?)",
		template.UserID, template.ProjectID, template.DurationHours, template.Description)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	template.ID = id

	return &template, nil
}

// Delete removes one of somebody's templates.
func (r *TemplateRepository) Delete(ctx context.Context, id, userID uint) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM entry_templates WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}
