package service

import (
	"context"
	"errors"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
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
	// Get time entry
	timesheet, err := s.timesheetRepository.GetByID(ctx, timesheetID)
	if err != nil {
		return nil, errors.New("timesheet not found")
	}

	// Check if new project exist
	newProject, err := s.projectRepository.GetByID(ctx, newProjectID)
	if err != nil {
		return nil, errors.New("new project not found")
	}

	// Check if the project is different
	if timesheet.ProjectID == newProject.ID {
		return nil, errors.New("cannot transfer timesheet to the same project")
	}

	// Update the project with the new time entry
	timesheet.ProjectID = newProject.ID
	updatedTimesheet, err := s.timesheetRepository.Update(ctx, timesheet)
	if err != nil {
		return nil, errors.New("failed to update timesheet during transfer")
	}

	return updatedTimesheet, nil
}

// GenerateProjectTimeReport creates a project report for multiple time entries
func (s *TimesheetDomainService) GenerateProjectTimeReport(
	ctx context.Context,
	projectID uint,
	startDate time.Time,
	endDate time.Time,
) (map[uint]float64, error) {
	// Check if project exist
	_, err := s.projectRepository.GetByID(ctx, projectID)
	if err != nil {
		return nil, errors.New("project not found")
	}

	// Get all time entry specified by a time range
	allTimesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{ProjectID: projectID})
	if err != nil {
		return nil, errors.New("failed to retrieve timesheets for report")
	}

	report := make(map[uint]float64)
	for _, ts := range allTimesheets {
		// Filter the data by time range
		if ts.Date.Unix() >= startDate.Unix() && ts.Date.Unix() <= endDate.Unix() {
			report[ts.UserID] += ts.DurationHours
		}
	}

	return report, nil
}
