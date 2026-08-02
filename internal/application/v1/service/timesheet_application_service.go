package service

import (
	"context"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

	// maxDailyHours caps what one user may book on a single day. It is the
	// value the environment configured; limits, when attached, may override it
	// with what an administrator set from the Settings screen.
	maxDailyHours float64
	limits        *LimitsProvider
}

// NewTimesheetApplicationService creates new instance
func NewTimesheetApplicationService(
	timesheetRepo repository.TimesheetRepository,
	userRepo repository.UserRepository,
	projectRepo repository.ProjectRepository,
	maxDailyHours float64,
) *TimesheetApplicationService {
	return &TimesheetApplicationService{
		timesheetRepository: timesheetRepo,
		userRepository:      userRepo,
		projectRepository:   projectRepo,
		maxDailyHours:       maxDailyHours,
	}
}

var _ TimesheetService = (*TimesheetApplicationService)(nil)

// CreateTimesheet processes the command to create a time entry
func (s *TimesheetApplicationService) CreateTimesheet(
	ctx context.Context,
	cmd command.CreateTimesheetCommand,
) (*command.CreateTimesheetCommandResult, error) {
	status := cmd.Status
	if status == "" {
		status = model.TimesheetStatusOpen
	}

	if err := validateTimesheet(cmd.Date, cmd.DurationHours, status); err != nil {
		return nil, err
	}

	if err := s.requireUserAndProject(ctx, cmd.UserID, cmd.ProjectID); err != nil {
		return nil, err
	}

	if err := s.checkDailyBudget(ctx, cmd.UserID, cmd.Date, cmd.DurationHours, 0); err != nil {
		return nil, err
	}

	createdTimesheet, err := s.timesheetRepository.Save(ctx, &model.Timesheet{
		UserID:        cmd.UserID,
		ProjectID:     normalizeProjectID(cmd.ProjectID),
		Date:          cmd.Date,
		DurationHours: cmd.DurationHours,
		Description:   cmd.Description,
		Status:        status,
	})
	if err != nil {
		return nil, err
	}

	return &command.CreateTimesheetCommandResult{
		Result: common.NewTimesheetResultFromModel(createdTimesheet)[0],
	}, nil
}

// GetTimesheet processes the query to get a time entry
func (s *TimesheetApplicationService) GetTimesheet(
	ctx context.Context,
	q query.GetTimesheetQuery,
) (*query.GetTimesheetQueryResult, error) {
	if q.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	timesheet, err := s.timesheetRepository.GetByID(ctx, q.ID)
	if err != nil {
		return nil, err
	}

	return &query.GetTimesheetQueryResult{
		Result: common.NewTimesheetResultFromModel(timesheet)[0],
	}, nil
}

// ListTimesheets processes the query to get all time entries matching a filter
func (s *TimesheetApplicationService) ListTimesheets(
	ctx context.Context,
	q query.ListTimesheetsQuery,
) (*query.ListTimesheetsQueryResult, error) {
	allTimesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    q.UserID,
		ProjectID: q.ProjectID,
		Status:    q.Status,
		StartDate: q.StartDate,
		EndDate:   q.EndDate,
	})
	if err != nil {
		return nil, err
	}

	return &query.ListTimesheetsQueryResult{
		Result:     common.NewTimesheetResultFromModel(allTimesheets...),
		TotalCount: uint(len(allTimesheets)),
	}, nil
}

// UpdateTimesheet processes the command to update a time entry
func (s *TimesheetApplicationService) UpdateTimesheet(
	ctx context.Context,
	cmd command.UpdateTimesheetCommand,
) (*command.UpdateTimesheetCommandResult, error) {
	if cmd.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	existingTimesheet, err := s.timesheetRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	// An approved entry is a signed-off record; changing its figures would
	// silently rewrite history, so only a status change is allowed.
	if existingTimesheet.Status == model.TimesheetStatusApproved && touchesFigures(cmd) {
		return nil, apperror.Conflictf("an approved timesheet can no longer be edited")
	}

	if cmd.UserID != nil {
		if _, err := s.userRepository.GetByID(ctx, *cmd.UserID); err != nil {
			return nil, err
		}

		existingTimesheet.UserID = *cmd.UserID
	}

	if cmd.ProjectID != nil {
		// 0 is how a client asks to remove the assignment again, which is what
		// makes an entry uncategorised.
		if *cmd.ProjectID == 0 {
			existingTimesheet.ProjectID = nil
		} else {
			if _, err := s.projectRepository.GetByID(ctx, *cmd.ProjectID); err != nil {
				return nil, err
			}

			existingTimesheet.ProjectID = normalizeProjectID(*cmd.ProjectID)
		}
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
		if err := validateStatusChange(existingTimesheet.Status, *cmd.Status); err != nil {
			return nil, err
		}

		existingTimesheet.Status = *cmd.Status
	}

	err = validateTimesheet(existingTimesheet.Date, existingTimesheet.DurationHours, existingTimesheet.Status)
	if err != nil {
		return nil, err
	}

	err = s.checkDailyBudget(ctx, existingTimesheet.UserID, existingTimesheet.Date,
		existingTimesheet.DurationHours, existingTimesheet.ID)
	if err != nil {
		return nil, err
	}

	updatedTimesheet, err := s.timesheetRepository.Update(ctx, existingTimesheet)
	if err != nil {
		return nil, err
	}

	return &command.UpdateTimesheetCommandResult{
		Result: common.NewTimesheetResultFromModel(updatedTimesheet)[0],
	}, nil
}

// DeleteTimesheet processes the command to delete a time entry
func (s *TimesheetApplicationService) DeleteTimesheet(ctx context.Context, cmd command.DeleteTimesheetCommand) error {
	if cmd.ID == 0 {
		return apperror.InvalidFields("id")
	}

	existing, err := s.timesheetRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return err
	}

	if existing.Status == model.TimesheetStatusApproved {
		return apperror.Conflictf("an approved timesheet can no longer be deleted")
	}

	return s.timesheetRepository.Delete(ctx, cmd.ID)
}

// requireUserAndProject validates the booking target. The project is optional:
// hours may be recorded without one and categorised later.
func (s *TimesheetApplicationService) requireUserAndProject(ctx context.Context, userID, projectID uint) error {
	if _, err := s.userRepository.GetByID(ctx, userID); err != nil {
		return err
	}

	if projectID == 0 {
		return nil
	}

	project, err := s.projectRepository.GetByID(ctx, projectID)
	if err != nil {
		return err
	}

	// A private project is one person's own category. Reporting it as "not
	// found" rather than "forbidden" keeps its existence private.
	if !project.VisibleTo(userID) {
		return apperror.NotFound("project", strconv.FormatUint(uint64(projectID), 10))
	}

	// Booking onto a finished project would corrupt its final figures.
	if project.Status != model.ProjectStatusActive {
		return apperror.Conflictf("project %q is %s and no longer accepts time entries",
			project.Name, project.Status)
	}

	return nil
}

// normalizeProjectID turns the "no project" sentinel into a nil pointer, so
// the absence is stored as NULL rather than as a dangling foreign key.
func normalizeProjectID(projectID uint) *uint {
	if projectID == 0 {
		return nil
	}

	return &projectID
}

// checkDailyBudget rejects a booking that would push the user over the daily
// cap. excludeID skips one entry so an update does not count itself twice.

func (s *TimesheetApplicationService) checkDailyBudget(
	ctx context.Context,
	userID uint,
	day time.Time,
	hours float64,
	excludeID uint,
) error {
	limit := s.dailyCap(ctx)
	if limit <= 0 {
		return nil
	}

	from := startOfDay(day)
	to := from.AddDate(0, 0, 1).Add(-time.Nanosecond)

	sameDay, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &from,
		EndDate:   &to,
	})
	if err != nil {
		return err
	}

	total := hours

	for _, entry := range sameDay {
		if entry.ID == excludeID {
			continue
		}

		total += entry.DurationHours
	}

	if total > limit {
		return apperror.Conflictf("booking %.2fh would total %.2fh on %s, over the %.2fh daily limit",
			hours, total, from.Format(time.DateOnly), limit)
	}

	return nil
}

// touchesFigures reports whether an update would change anything other than
// the status.
func touchesFigures(cmd command.UpdateTimesheetCommand) bool {
	return cmd.UserID != nil || cmd.ProjectID != nil || cmd.Date != nil ||
		cmd.DurationHours != nil || cmd.Description != nil
}

// validateStatusChange enforces the timesheet lifecycle:
// open -> submitted -> approved/rejected, and rejected -> open for rework.
func validateStatusChange(current, next string) error {
	if current == next {
		return nil
	}

	allowed := map[string][]string{
		model.TimesheetStatusOpen:      {model.TimesheetStatusSubmitted},
		model.TimesheetStatusSubmitted: {model.TimesheetStatusApproved, model.TimesheetStatusRejected},
		model.TimesheetStatusRejected:  {model.TimesheetStatusOpen},
		model.TimesheetStatusApproved:  {},
	}

	for _, candidate := range allowed[current] {
		if candidate == next {
			return nil
		}
	}

	return apperror.Conflictf("cannot change timesheet status from %q to %q", current, next)
}

func validateTimesheet(date time.Time, hours float64, status string) error {
	var invalid []string

	if date.IsZero() {
		invalid = append(invalid, "date")
	}

	// An entry of zero hours carries no information, and a full day is the
	// most that can be booked on one entry.
	if hours <= 0 || hours > 24 {
		invalid = append(invalid, "durationHours")
	}

	switch status {
	case model.TimesheetStatusOpen, model.TimesheetStatusSubmitted,
		model.TimesheetStatusApproved, model.TimesheetStatusRejected:
	default:
		invalid = append(invalid, "status")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}

// dailyCap is the administered booking limit, or the one the environment set.
func (s *TimesheetApplicationService) dailyCap(ctx context.Context) float64 {
	if s.limits == nil {
		return s.maxDailyHours
	}

	return s.limits.Limits(ctx).MaxDailyHours
}

// WithLimits attaches the administered limits, so a change to the daily cap
// applies without a restart.
func (s *TimesheetApplicationService) WithLimits(limits *LimitsProvider) *TimesheetApplicationService {
	s.limits = limits

	return s
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
