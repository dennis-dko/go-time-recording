package service

import (
	"context"
	"strconv"
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

// requireVisible refuses a project the viewer is not allowed to know about.
//
// A not-found rather than a refusal, so a private project's existence is not
// revealed by the difference between the two status codes - the same reading the
// application layer takes.
//
// A viewer id of zero means authentication is switched off, which is the local
// trial case and sees everything.
func requireVisible(project *model.Project, viewerID uint) error {
	if viewerID == 0 || project.VisibleTo(viewerID) {
		return nil
	}

	return apperror.NotFound("project", strconv.FormatUint(uint64(project.ID), 10))
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

	// An approved entry is a signed-off record; moving its hours to another
	// project would silently rewrite an already-reported total.
	newProject, err := s.projectRepository.GetByID(ctx, newProjectID)
	if err != nil {
		return nil, err
	}

	if err := requireVisible(newProject, viewerID); err != nil {
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
	if err := requireVisible(project, viewerID); err != nil {
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
