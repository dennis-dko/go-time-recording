package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// maxTemplatesPerUser bounds how many templates one person can keep. A picker
// longer than this is a list to search rather than a shortcut, and a client
// saving in a loop would otherwise grow the table for as long as it ran.
const maxTemplatesPerUser = 50

// TemplateService keeps the bookings somebody saves to fill the booking form with.
//
// Each person's own, like their entries: nobody lists, fills from or removes
// anybody else's.
type TemplateService struct {
	templates repository.TemplateRepository
	projects  repository.ProjectRepository
}

// NewTemplateService needs the projects as well, because a template only names
// one the person could book on.
func NewTemplateService(
	templates repository.TemplateRepository,
	projects repository.ProjectRepository,
) *TemplateService {
	return &TemplateService{templates: templates, projects: projects}
}

// Templates returns somebody's templates, in the order they were saved.
func (s *TemplateService) Templates(ctx context.Context, userID uint) ([]model.EntryTemplate, error) {
	return s.templates.ForUser(ctx, userID)
}

// Keep saves a template, or answers with the one already kept that fills the form
// the same way - a second press of the button is not a second template.
//
// Held to what the booking it fills is held to, wherever a template can know it:
// hours one entry can hold, a description no longer than one, and a project the
// person can book on. Without the last, a template would answer whether a
// project id exists, and filling the form from one naming a closed project would
// put a choice into it that the form does not offer.
func (s *TemplateService) Keep(ctx context.Context, template model.EntryTemplate) (*model.EntryTemplate, error) {
	if template.ProjectID != nil && *template.ProjectID == 0 {
		template.ProjectID = nil
	}

	if template.Description != nil {
		written := strings.TrimSpace(*template.Description)
		template.Description = &written

		if written == "" {
			template.Description = nil
		}
	}

	var invalid []string

	// Inside the range rather than outside it, so NaN is refused: see
	// validateTimesheet.
	if hours := template.DurationHours; hours != nil &&
		!(*hours >= model.MinBookableHours && *hours <= model.HoursPerDay) {
		invalid = append(invalid, "durationHours")
	}

	if template.Description != nil && model.TooLong(*template.Description, model.MaxDescriptionLength) {
		invalid = append(invalid, "description")
	}

	if len(invalid) > 0 {
		return nil, apperror.InvalidFields(invalid...)
	}

	if template.ProjectID == nil && template.DurationHours == nil && template.Description == nil {
		return nil, apperror.Invalidf("a template has to fill in something: a project, hours or a description").
			WithCode("templateEmpty")
	}

	if template.ProjectID != nil {
		project, err := s.projects.GetByID(ctx, *template.ProjectID)
		if err != nil {
			return nil, err
		}

		if err := domainservice.RequireVisible(project, template.UserID); err != nil {
			return nil, err
		}

		if project.Status != model.ProjectStatusActive {
			return nil, apperror.Conflictf("project %q is %s and no longer accepts time entries",
				project.Name, project.Status).
				WithCode("projectClosedForBooking", project.Name, project.Status)
		}
	}

	kept, err := s.templates.ForUser(ctx, template.UserID)
	if err != nil {
		return nil, err
	}

	for _, existing := range kept {
		if samePointee(existing.ProjectID, template.ProjectID) &&
			samePointee(existing.DurationHours, template.DurationHours) &&
			samePointee(existing.Description, template.Description) {
			return &existing, nil
		}
	}

	if len(kept) >= maxTemplatesPerUser {
		return nil, apperror.Conflictf("at most %d templates per person; delete one first", maxTemplatesPerUser).
			WithCode("tooManyTemplates", maxTemplatesPerUser)
	}

	return s.templates.Save(ctx, template)
}

// Remove deletes one of somebody's templates. Anybody else's answers as not
// found, the same as one that never existed.
func (s *TemplateService) Remove(ctx context.Context, id, userID uint) error {
	found, err := s.templates.Delete(ctx, id, userID)
	if err != nil {
		return err
	}

	if !found {
		return apperror.NotFound("template", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

// samePointee reports whether two optional values are the same, counting two
// absent ones as equal.
func samePointee[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}
