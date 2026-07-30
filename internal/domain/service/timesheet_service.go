package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// TimesheetDomainService encapsulates domain logic
type TimesheetDomainService struct {
	timesheetRepository repository.TimesheetRepository
	projectRepository   repository.ProjectRepository
	userRepository      repository.UserRepository
}

// NewTimesheetDomainService creates new instance
func NewTimesheetDomainService(
	timesheetRepo repository.TimesheetRepository,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
) *TimesheetDomainService {
	return &TimesheetDomainService{
		timesheetRepository: timesheetRepo,
		projectRepository:   projectRepo,
		userRepository:      userRepo,
	}
}

// TransferTimesheetToProject moves a time entry from one project to another
func (s *TimesheetDomainService) TransferTimesheetToProject(
	ctx context.Context,
	timesheetID uint,
	newProjectID uint,
) (*model.Timesheet, error) {
	timesheet, err := s.timesheetRepository.GetByID(ctx, timesheetID)
	if err != nil {
		return nil, err
	}

	// An approved entry is a signed-off record; moving its hours to another
	// project would silently rewrite an already-reported total.
	if timesheet.Status == model.TimesheetStatusApproved {
		return nil, apperror.Conflictf("an approved timesheet can no longer be transferred")
	}

	newProject, err := s.projectRepository.GetByID(ctx, newProjectID)
	if err != nil {
		return nil, err
	}

	if timesheet.ProjectID == newProject.ID {
		return nil, apperror.Conflictf("the timesheet is already booked on project %q", newProject.Name)
	}

	if newProject.Status != model.ProjectStatusActive {
		return nil, apperror.Conflictf("project %q is %s and no longer accepts time entries",
			newProject.Name, newProject.Status)
	}

	timesheet.ProjectID = newProject.ID

	updatedTimesheet, err := s.timesheetRepository.Update(ctx, timesheet)
	if err != nil {
		return nil, err
	}

	return updatedTimesheet, nil
}

// GenerateProjectTimeReport totals the hours each user booked on a project
// within the given range. The range is inclusive on both ends.
func (s *TimesheetDomainService) GenerateProjectTimeReport(
	ctx context.Context,
	projectID uint,
	startDate time.Time,
	endDate time.Time,
) (map[uint]float64, error) {
	if _, err := s.projectRepository.GetByID(ctx, projectID); err != nil {
		return nil, err
	}

	// The date range is pushed into the filter rather than applied afterwards,
	// so a SQL repository can narrow the result server-side.
	timesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		ProjectID: projectID,
		StartDate: &startDate,
		EndDate:   &endDate,
	})
	if err != nil {
		return nil, err
	}

	report := make(map[uint]float64, len(timesheets))
	for _, ts := range timesheets {
		report[ts.UserID] += ts.DurationHours
	}

	return report, nil
}
