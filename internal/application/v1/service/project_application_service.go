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

// ProjectService service interface
type ProjectService interface {
	CreateProject(ctx context.Context, cmd command.CreateProjectCommand) (*command.CreateProjectCommandResult, error)
	GetProject(ctx context.Context, q query.GetProjectQuery) (*query.GetProjectQueryResult, error)
	ListProjects(ctx context.Context, q query.ListProjectsQuery) (*query.ListProjectsQueryResult, error)
	UpdateProject(ctx context.Context, cmd command.UpdateProjectCommand) (*command.UpdateProjectCommandResult, error)
	DeleteProject(ctx context.Context, cmd command.DeleteProjectCommand) error
}

// ProjectApplicationService application service for projects
type ProjectApplicationService struct {
	projectRepository   repository.ProjectRepository
	timesheetRepository repository.TimesheetRepository

	// timers is only for the deletion path: a project somebody is timing against
	// cannot be removed. See DeleteProject.
	timers repository.TimerRepository
}

// NewProjectApplicationService creates new instance
func NewProjectApplicationService(
	projectRepo repository.ProjectRepository,
	timesheetRepo repository.TimesheetRepository,
	timers repository.TimerRepository,
) *ProjectApplicationService {
	return &ProjectApplicationService{
		projectRepository:   projectRepo,
		timesheetRepository: timesheetRepo,
		timers:              timers,
	}
}

var _ ProjectService = (*ProjectApplicationService)(nil)

// CreateProject processes the command to create a project
func (s *ProjectApplicationService) CreateProject(
	ctx context.Context,
	cmd command.CreateProjectCommand,
) (*command.CreateProjectCommandResult, error) {
	status := cmd.Status
	if status == "" {
		status = model.ProjectStatusActive
	}

	startDate := cmd.StartDate

	// A project is one person's way of organising their own hours, not a plan
	// somebody signed off, so it does not need a start date: defaulting it keeps the
	// form down to just a name. This used to apply only to a private category, which
	// is what every project is now.
	// The same shape a posted one arrives in. A date sent as "2026-07-03" parses
	// to midnight UTC and is stored that way; this used to default to midnight in
	// whatever zone the server runs in, which is the same field holding two
	// different things - and one of them a day early once a driver normalises it.
	if startDate.IsZero() {
		startDate = model.CalendarDay(time.Now())
	}

	if err := validateProject(cmd.Name, status, cmd.Description, startDate, cmd.EndDate); err != nil {
		return nil, err
	}

	createdProject, err := s.projectRepository.Save(ctx, &model.Project{
		Name:        cmd.Name,
		Description: cmd.Description,
		StartDate:   startDate,
		EndDate:     cmd.EndDate,
		Status:      status,
		OwnerID:     cmd.OwnerID,
	})
	if err != nil {
		return nil, err
	}

	return &command.CreateProjectCommandResult{
		Result: common.NewProjectResultFromModel(createdProject)[0],
	}, nil
}

// GetProject processes the query to get a project
func (s *ProjectApplicationService) GetProject(
	ctx context.Context,
	q query.GetProjectQuery,
) (*query.GetProjectQueryResult, error) {
	if q.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	project, err := s.projectRepository.GetByID(ctx, q.ID)
	if err != nil {
		return nil, err
	}

	if err := requireVisible(project, q.ViewerID); err != nil {
		return nil, err
	}

	return &query.GetProjectQueryResult{
		Result: common.NewProjectResultFromModel(project)[0],
	}, nil
}

// requireVisible hides someone else's private project behind a not-found,
// so its existence is not revealed by a different status code.
func requireVisible(project *model.Project, viewerID uint) error {
	if viewerID == 0 || project.VisibleTo(viewerID) {
		return nil
	}

	return apperror.NotFound("project", strconv.FormatUint(uint64(project.ID), 10))
}

// ListProjects processes the query to get all projects, optionally filtered
// by status.
func (s *ProjectApplicationService) ListProjects(
	ctx context.Context,
	q query.ListProjectsQuery,
) (*query.ListProjectsQueryResult, error) {
	allProjects, err := s.projectRepository.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	matching := make([]*model.Project, 0, len(allProjects))

	for _, project := range allProjects {
		// A project belongs to one person; nobody else gets to see that it exists,
		// let alone what it is called. A viewer of zero is enforcement switched off,
		// which every other read takes the same way.
		if q.ViewerID != 0 && !project.VisibleTo(q.ViewerID) {
			continue
		}

		if q.Status != "" && project.Status != q.Status {
			continue
		}

		matching = append(matching, project)
	}

	return &query.ListProjectsQueryResult{
		Result:     common.NewProjectResultFromModel(matching...),
		TotalCount: uint(len(matching)),
	}, nil
}

// UpdateProject processes the command to update a project
func (s *ProjectApplicationService) UpdateProject(
	ctx context.Context,
	cmd command.UpdateProjectCommand,
) (*command.UpdateProjectCommandResult, error) {
	if cmd.ID == 0 {
		return nil, apperror.InvalidFields("id")
	}

	existingProject, err := s.projectRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	if err := requireVisible(existingProject, cmd.ActorID); err != nil {
		return nil, err
	}

	if cmd.Name != nil {
		existingProject.Name = *cmd.Name
	}

	if cmd.Description != nil {
		existingProject.Description = cmd.Description
	}

	if cmd.StartDate != nil {
		existingProject.StartDate = *cmd.StartDate
	}

	if cmd.EndDate != nil {
		existingProject.EndDate = cmd.EndDate
	}

	if cmd.Status != nil {
		if err := s.validateStatusTransition(ctx, existingProject, *cmd.Status); err != nil {
			return nil, err
		}

		existingProject.Status = *cmd.Status
	}

	err = validateProject(existingProject.Name, existingProject.Status, existingProject.Description,
		existingProject.StartDate, existingProject.EndDate)
	if err != nil {
		return nil, err
	}

	updatedProject, err := s.projectRepository.Update(ctx, existingProject)
	if err != nil {
		return nil, err
	}

	return &command.UpdateProjectCommandResult{
		Result: common.NewProjectResultFromModel(updatedProject)[0],
	}, nil
}

// validateStatusTransition mirrors the archiving rule enforced by the domain
// service: a project may only be archived once it is completed and has no
// open time entries left.
func (s *ProjectApplicationService) validateStatusTransition(
	ctx context.Context,
	project *model.Project,
	newStatus string,
) error {
	if newStatus != model.ProjectStatusArchived {
		return nil
	}

	if project.Status != model.ProjectStatusCompleted {
		return apperror.Conflictf("a project can only be archived once its status is %q",
			model.ProjectStatusCompleted).
			WithCode("archiveNeedsCompleted", model.ProjectStatusCompleted)
	}

	// Entries no longer stand in the way. The rule was about open ones still
	// expecting edits; an entry has no state any more, so that would mean every
	// entry there is - and refusing to archive a finished project because time was
	// booked against it is backwards. Deleting one still refuses while any exist.
	return nil
}

// DeleteProject refuses to delete a project that still has time entries, so
// booked hours cannot be orphaned.
func (s *ProjectApplicationService) DeleteProject(ctx context.Context, cmd command.DeleteProjectCommand) error {
	if cmd.ID == 0 {
		return apperror.InvalidFields("id")
	}

	project, err := s.projectRepository.GetByID(ctx, cmd.ID)
	if err != nil {
		return err
	}

	if err := requireVisible(project, cmd.ActorID); err != nil {
		return err
	}

	entries, err := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{ProjectID: cmd.ID})
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		return apperror.Conflictf("cannot delete a project that still has %d time entries", len(entries)).
			WithCode("projectHasEntries", len(entries))
	}

	// A clock running against it counts too, for the same reason and one more.
	// running_timers.project_id is a foreign key with no ON DELETE behaviour: the
	// delete is refused outright by PostgreSQL and MySQL, and accepted by SQLite,
	// which leaves somebody with a clock that can never be stopped. Refused here
	// rather than left to the engine, so the answer is the same everywhere and says
	// what to do about it.
	timing, err := s.timers.CountByProject(ctx, cmd.ID)
	if err != nil {
		return err
	}

	if timing > 0 {
		return apperror.Conflictf(
			"%d person(s) are timing against this project; it can be deleted once they stop",
			timing).WithCode("projectIsBeingTimed", timing)
	}

	return s.projectRepository.Delete(ctx, cmd.ID)
}

func validateProject(
	name, status string,
	description *string,
	startDate time.Time,
	endDate *time.Time,
) error {
	var invalid []string

	// Too long is refused here rather than by the column, which only PostgreSQL
	// and MySQL enforce - so without this it is stored on SQLite and answered
	// with a driver error everywhere else.
	if name == "" || model.TooLong(name, model.MaxNameLength) {
		invalid = append(invalid, "name")
	}

	// TEXT has no width, so nothing below this would refuse a description large
	// enough to slow every listing that renders it.
	if description != nil && model.TooLong(*description, model.MaxDescriptionLength) {
		invalid = append(invalid, "description")
	}

	if startDate.IsZero() {
		invalid = append(invalid, "startDate")
	}

	if endDate != nil && !startDate.IsZero() && endDate.Before(startDate) {
		invalid = append(invalid, "endDate")
	}

	switch status {
	case model.ProjectStatusActive, model.ProjectStatusCompleted, model.ProjectStatusArchived:
	default:
		invalid = append(invalid, "status")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}
