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

// TransferTimesheetToProject moves a time entry from one project to another.
//
// viewerID is who is asking, because the target may be somebody else's private
// category: without this check a timesheets:transfer holder could move an entry
// onto a project they are not allowed to see, and read its name back out of the
// response.
func (s *TimesheetDomainService) TransferTimesheetToProject(
	ctx context.Context,
	timesheetID uint,
	newProjectID uint,
	viewerID uint,
) (*model.Timesheet, error) {
	timesheet, err := s.timesheetRepository.GetByID(ctx, timesheetID)
	if err != nil {
		return nil, err
	}

	newProject, err := s.projectRepository.GetByID(ctx, newProjectID)
	if err != nil {
		return nil, err
	}

	if err := RequireVisible(newProject, viewerID); err != nil {
		return nil, err
	}

	if timesheet.HasProject() && *timesheet.ProjectID == newProject.ID {
		return nil, apperror.Conflictf("the timesheet is already booked on project %q", newProject.Name).
			WithCode("entryAlreadyOnProject", newProject.Name)
	}

	if newProject.Status != model.ProjectStatusActive {
		return nil, apperror.Conflictf("project %q is %s and no longer accepts time entries",
			newProject.Name, newProject.Status).
			WithCode("projectClosedForBooking", newProject.Name, newProject.Status)
	}

	// Transferring is also how an uncategorised entry gets its first project.
	projectID := newProject.ID
	timesheet.ProjectID = &projectID

	updatedTimesheet, err := s.timesheetRepository.Update(ctx, timesheet)
	if err != nil {
		return nil, err
	}

	return updatedTimesheet, nil
}

// ProjectScope says which projects an evaluation covers.
//
// A struct rather than an id, because an id has only one spare value and there are
// two things to say with it: zero has to mean "every project", so "only the hours
// that were never given a project" had nowhere to live - and so the one evaluation
// people actually wanted, what did I do that is not on anything, could not be asked
// for at all. Splitting it makes the ambiguity impossible to write down again.
type ProjectScope struct {
	// ProjectID names one project. Zero means every project this person books on.
	ProjectID uint

	// Unassigned narrows it to the entries that never got a project. Only
	// meaningful while ProjectID is zero; naming a project and asking for the ones
	// without would be a contradiction, so the project wins.
	Unassigned bool
}

// GenerateOwnTimeReport totals what one person booked over a range: on one
// project, across all of them, or only on the hours that belong to none.
//
// Separate from GenerateProjectTimeReport rather than a parameter on it, because
// the two answer different questions. That one is about a project and has to decide
// whether the caller may see it at all; this one is about a person and is always
// about the caller, so there is nothing to hide from them.
func (s *TimesheetDomainService) GenerateOwnTimeReport(
	ctx context.Context,
	scope ProjectScope,
	startDate time.Time,
	endDate time.Time,
	userID uint,
) (float64, error) {
	// A named project is still checked, because a project id is something the
	// caller supplies and somebody else's project is not theirs to total.
	if scope.ProjectID != 0 {
		project, err := s.projectRepository.GetByID(ctx, scope.ProjectID)
		if err != nil {
			return 0, err
		}

		if err := RequireVisible(project, userID); err != nil {
			return 0, err
		}
	}

	timesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:         userID,
		ProjectID:      scope.ProjectID,
		WithoutProject: scope.ProjectID == 0 && scope.Unassigned,
		StartDate:      &startDate,
		EndDate:        &endDate,
	})
	if err != nil {
		return 0, err
	}

	var total float64
	for _, ts := range timesheets {
		total += ts.DurationHours
	}

	return total, nil
}

// GenerateProjectTimeReport totals the hours booked on a project, per user.
//
// onlyUserID narrows it to one person; zero means everybody. Everybody is not
// something a default role may ask for. The report used to total what every
// colleague had booked on the project, for anyone who could open it at all - which
// is precisely what nobody is meant to see. Whether the caller may is decided by the
// handler and arrives here as this parameter, so the rule is applied in one place
// rather than assumed in two.
//
// viewerID is a different question, and both are needed: it decides whether the
// project may be seen at all, which is what keeps somebody's private category
// private.
// within the given range. The range is inclusive on both ends.
func (s *TimesheetDomainService) GenerateProjectTimeReport(
	ctx context.Context,
	projectID uint,
	startDate time.Time,
	endDate time.Time,
	viewerID uint,
	onlyUserID uint,
) (map[uint]float64, error) {
	project, err := s.projectRepository.GetByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// The project record was already hidden from anyone it does not belong to;
	// the hours booked against it were not. Reading a report was enough to learn
	// what somebody had recorded against their own private category, and how
	// much.
	if err := RequireVisible(project, viewerID); err != nil {
		return nil, err
	}

	// The date range is pushed into the filter rather than applied afterwards,
	// so a SQL repository can narrow the result server-side.
	timesheets, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		ProjectID: projectID,
		StartDate: &startDate,
		EndDate:   &endDate,
		// Narrowed in the query rather than after it, so the rows for other people
		// are never read at all.
		UserID: onlyUserID,
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
