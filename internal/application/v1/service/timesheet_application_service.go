package service

import (
	"context"
	"errors"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// TimesheetService service interface
type TimesheetService interface {
	CreateTimesheet(ctx context.Context, cmd command.CreateTimesheetCommand) (*command.CreateTimesheetCommandResult, error)
	GetTimesheet(ctx context.Context, q query.GetTimesheetQuery) (*query.GetTimesheetQueryResult, error)
	ListTimesheets(ctx context.Context, q query.ListTimesheetsQuery) (*query.ListTimesheetsQueryResult, error)
	UpdateTimesheet(ctx context.Context, cmd command.UpdateTimesheetCommand) (*command.UpdateTimesheetCommandResult, error)
	DeleteTimesheet(ctx context.Context, cmd command.DeleteTimesheetCommand) error
}

// TimesheetApplicationService application service for time entries
type TimesheetApplicationService struct {
	timesheetRepository repository.TimesheetRepository
	userRepository      repository.UserRepository
	projectRepository   repository.ProjectRepository
}

// NewTimesheetApplicationService creates new instance
func NewTimesheetApplicationService(
	timesheetRepo repository.TimesheetRepository,
	userRepo repository.UserRepository,
	projectRepo repository.ProjectRepository,
) *TimesheetApplicationService {
	return &TimesheetApplicationService{
		timesheetRepository: timesheetRepo,
		userRepository:      userRepo,
		projectRepository:   projectRepo,
	}
}

// CreateTimesheet processes the command to create a time entry
func (s *TimesheetApplicationService) CreateTimesheet(ctx context.Context, cmd command.CreateTimesheetCommand) (*command.CreateTimesheetCommandResult, error) {
	// Check if user and project exists
	_, err := s.userRepository.GetByID(ctx, cmd.UserID)
	if err != nil {
		return nil, errors.New("user not found")
	}
	_, err = s.projectRepository.GetByID(ctx, cmd.ProjectID)
	if err != nil {
		return nil, errors.New("project not found")
	}

	// Create timesheet model
	timesheetModel := &model.Timesheet{
		UserID:        cmd.UserID,
		ProjectID:     cmd.ProjectID,
		Date:          cmd.Date,
		DurationHours: cmd.DurationHours,
		Description:   cmd.Description,
		Status:        cmd.Status,
	}

	createdTimesheet, err := s.timesheetRepository.Save(ctx, timesheetModel)
	if err != nil {
		return nil, err
	}

	// Convert timesheet model to response
	appResponse := &command.CreateTimesheetCommandResult{
		Result: common.NewTimesheetResultFromModel(createdTimesheet)[0],
	}

	return appResponse, nil
}

// GetTimesheet processes the query to get a time entry
func (s *TimesheetApplicationService) GetTimesheet(ctx context.Context, q query.GetTimesheetQuery) (*query.GetTimesheetQueryResult, error) {
	if q.ID == 0 {
		return nil, errors.New("timesheet ID cannot be empty")
	}

	getTimesheet, err := s.timesheetRepository.GetByID(ctx, q.ID)
	if err != nil {
		return nil, err
	}

	// Convert timesheet model to response
	appResponse := &query.GetTimesheetQueryResult{
		Result: common.NewTimesheetResultFromModel(getTimesheet)[0],
	}

	return appResponse, nil
}

// ListTimesheets processes the query to get all time entries
func (s *TimesheetApplicationService) ListTimesheets(ctx context.Context, q query.ListTimesheetsQuery) (*query.ListTimesheetsQueryResult, error) {
	// Get all time entries by filter
	filter := repository.TimesheetFilter{
		UserID:    q.UserID,
		ProjectID: q.ProjectID,
		Status:    q.Status,
		StartDate: q.StartDate,
		EndDate:   q.EndDate,
	}
	allTimesheets, err := s.timesheetRepository.GetByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert timesheet model to response
	appResponse := &query.ListTimesheetsQueryResult{
		Result: common.NewTimesheetResultFromModel(allTimesheets...),
	}

	return appResponse, nil
}

// UpdateTimesheet processes the command to update a time entry
func (s *TimesheetApplicationService) UpdateTimesheet(ctx context.Context, cmd command.UpdateTimesheetCommand) (*command.UpdateTimesheetCommandResult, error) {
	if cmd.ID == 0 {
		return nil, errors.New("timesheet ID cannot be empty for update")
	}

	existingTimesheet, err := s.timesheetRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	// Check if user and project exists
	if cmd.UserID != nil {
		_, err := s.userRepository.GetByID(ctx, *cmd.UserID)
		if err != nil {
			return nil, errors.New("new user ID not found")
		}
		existingTimesheet.UserID = *cmd.UserID
	}
	if cmd.ProjectID != nil {
		_, err := s.projectRepository.GetByID(ctx, *cmd.ProjectID)
		if err != nil {
			return nil, errors.New("new project ID not found")
		}
		existingTimesheet.ProjectID = *cmd.ProjectID
	}
	if cmd.Date != nil {
		existingTimesheet.Date = *cmd.Date
	}
	if cmd.DurationHours != nil {
		existingTimesheet.DurationHours = *cmd.DurationHours
	}
	if cmd.Description != nil {
		existingTimesheet.Description = cmd.Description
	}
	if cmd.Status != nil {
		// Check that the new status is not 'open' again if it was set to 'approved' before
		if existingTimesheet.Status == model.TimesheetStatusApproved && *cmd.Status == model.TimesheetStatusOpen {
			return nil, errors.New("cannot revert approved timesheet to open status")
		}
		existingTimesheet.Status = *cmd.Status
	}

	updatedTimesheet, err := s.timesheetRepository.Update(ctx, existingTimesheet)
	if err != nil {
		return nil, err
	}

	// Convert timesheet model to response
	appResponse := &command.UpdateTimesheetCommandResult{
		Result: common.NewTimesheetResultFromModel(updatedTimesheet)[0],
	}

	return appResponse, nil
}

// DeleteTimesheet processes the command to delete a time entry
func (s *TimesheetApplicationService) DeleteTimesheet(ctx context.Context, cmd command.DeleteTimesheetCommand) error {
	if cmd.ID == 0 {
		return errors.New("timesheet ID cannot be empty for delete")
	}
	return s.timesheetRepository.Delete(ctx, cmd.ID)
}
