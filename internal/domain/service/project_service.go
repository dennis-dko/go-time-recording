package service

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// ProjectDomainService holds the project rule that is more than a field check:
// archiving, which needs the project completed and the caller able to see it.
type ProjectDomainService struct {
	projectRepository repository.ProjectRepository
}

// NewProjectDomainService works on the projects alone: nothing it decides
// depends on the hours booked against one.
func NewProjectDomainService(projectRepo repository.ProjectRepository) *ProjectDomainService {
	return &ProjectDomainService{projectRepository: projectRepo}
}

// ArchiveProject archives the project once it is completed. Time booked against
// it is no reason to refuse; the note in the body says why.
//
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

	if err := RequireVisible(project, viewerID); err != nil {
		return nil, err
	}

	if project.Status != model.ProjectStatusCompleted {
		return nil, apperror.Conflictf("a project can only be archived once its status is %q",
			model.ProjectStatusCompleted).
			WithCode("archiveNeedsCompleted", model.ProjectStatusCompleted)
	}

	// Entries are no longer checked here. The rule was that open ones still expected
	// edits and archiving would strand them - but an entry has no state any more, so
	// "open" would mean every entry there is, and refusing to archive a finished
	// project because time was booked against it is backwards: that is what
	// archiving is for. The entries stay readable, and deleting a project still
	// refuses while any exist.

	project.Status = model.ProjectStatusArchived

	updatedProject, err := s.projectRepository.Update(ctx, project)
	if err != nil {
		return nil, err
	}

	return updatedProject, nil
}
