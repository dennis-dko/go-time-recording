package service

import (
	"context"
	"errors"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
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

// ArchiveProject archives the project
func (s *ProjectDomainService) ArchiveProject(ctx context.Context, projectID uint) (*model.Project, error) {
	project, err := s.projectRepository.GetByID(ctx, projectID)
	if err != nil {
		return nil, errors.New("project not found")
	}

	// Archive the project if it's completed
	if project.Status != model.ProjectStatusCompleted {
		return nil, errors.New("project can only be archived if its status is 'completed'")
	}

	// Check if open timesheets exist for this project
	timesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{ProjectID: projectID})
	if err != nil {
		return nil, errors.New("failed to check for open timesheets")
	}
	if len(timesheets) > 0 {
		for _, timesheet := range timesheets {
			if timesheet.Status == model.TimesheetStatusOpen {
				return nil, errors.New("cannot archive project with open timesheets")
			}
		}
	}

	// Change the status
	project.Status = model.ProjectStatusArchived
	updatedProject, err := s.projectRepository.Update(ctx, project)
	if err != nil {
		return nil, errors.New("failed to archive the project")
	}

	return updatedProject, nil
}
