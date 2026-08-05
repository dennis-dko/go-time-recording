package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// ProjectDomainService encapsulates domain logic
type ProjectDomainService struct {
	projectRepository   repository.ProjectRepository
	timesheetRepository repository.TimesheetRepository
}

// NewProjectDomainService creates new instance
func NewProjectDomainService(
	projectRepo repository.ProjectRepository,
	timesheetRepo repository.TimesheetRepository,
) *ProjectDomainService {
	return &ProjectDomainService{
		projectRepository:   projectRepo,
		timesheetRepository: timesheetRepo,
	}
}

// ArchiveProject archives the project once it is completed and has no open
// time entries left.
// viewerID is who is asking: archiving somebody else's private category would
// take their own project away from them, and the request would also confirm that
// it exists.
func (s *ProjectDomainService) ArchiveProject(
	ctx context.Context,
	projectID uint,
	viewerID uint,
) (*model.Project, error) {
	// Repository errors are already classified, so they are returned as-is
	// rather than flattened into a generic message, which would cost the
	// caller its 404.
	project, err := s.projectRepository.GetByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	if err := requireVisible(project, viewerID); err != nil {
		return nil, err
	}

	if project.Status != model.ProjectStatusCompleted {
		return nil, apperror.Conflictf("a project can only be archived once its status is %q",
			model.ProjectStatusCompleted).
			WithCode("archiveNeedsCompleted", model.ProjectStatusCompleted)
	}

	// Open entries still expect edits, so archiving would strand them.
	openEntries, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		ProjectID: projectID,
		Status:    model.TimesheetStatusOpen,
	})
	if err != nil {
		return nil, err
	}

	if len(openEntries) > 0 {
		return nil, apperror.Conflictf("cannot archive a project with %d open time entries", len(openEntries)).
			WithCode("archiveHasOpenEntries", len(openEntries))
	}

	project.Status = model.ProjectStatusArchived

	updatedProject, err := s.projectRepository.Update(ctx, project)
	if err != nil {
		return nil, err
	}

	return updatedProject, nil
}
