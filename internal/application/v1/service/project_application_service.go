package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// ProjectService is what the project handler may do with projects. A project
// the caller may not see is not found rather than refused, and a list leaves it
// out.
type ProjectService interface {
	CreateProject(ctx context.Context, cmd command.CreateProjectCommand) (*command.CreateProjectCommandResult, error)
	GetProject(ctx context.Context, q query.GetProjectQuery) (*query.GetProjectQueryResult, error)
	ListProjects(ctx context.Context, q query.ListProjectsQuery) (*query.ListProjectsQueryResult, error)
	UpdateProject(ctx context.Context, cmd command.UpdateProjectCommand) (*command.UpdateProjectCommandResult, error)
	DeleteProject(ctx context.Context, cmd command.DeleteProjectCommand) error
}

// ProjectApplicationService is the ProjectService the application runs with.
// Visibility is checked on every call, and a project is not deleted while hours
// are booked on it or a clock is running against it.
type ProjectApplicationService struct {
	projectRepository   repository.ProjectRepository
	timesheetRepository repository.TimesheetRepository

	// timers is only for the deletion path: a project somebody is timing against
	// cannot be removed. See DeleteProject.
	timers repository.TimerRepository
}

// NewProjectApplicationService needs the time entries and the running clocks
// as well as the projects, for the two refusals DeleteProject makes.
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
	// And the creator's day rather than the server's, which a caller that did not
	// say has no default for: a guess here is a project on the wrong day.
	if startDate.IsZero() {
		if cmd.Today.IsZero() {
			return nil, apperror.Internal(errors.New("a project with no start date needs its creator's day"))
		}

		startDate = model.CalendarDay(cmd.Today)
	}

	if err := validateProject(cmd.Name, status, cmd.Description, startDate, cmd.EndDate); err != nil {
		return nil, err
	}

	if err := validateBudget(cmd.BudgetHours); err != nil {
		return nil, err
	}

	if err := validateRate(cmd.HourlyRate); err != nil {
		return nil, err
	}

	currency, err := currencyCode(cmd.Currency)
	if err != nil {
		return nil, err
	}

	createdProject, err := s.projectRepository.Save(ctx, &model.Project{
		Name:        cmd.Name,
		Description: cmd.Description,
		StartDate:   startDate,
		EndDate:     cmd.EndDate,
		Status:      status,
		OwnerID:     cmd.OwnerID,
		BudgetHours: budgetOrNone(cmd.BudgetHours),
		Billable:    cmd.Billable,
		HourlyRate:  hundredths(cmd.HourlyRate),
		Currency:    currency,
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

	if err := domainservice.RequireVisible(project, q.ViewerID); err != nil {
		return nil, err
	}

	result := common.NewProjectResultFromModel(project)

	if err := s.withBookedHours(ctx, q.ViewerID, result); err != nil {
		return nil, err
	}

	return &query.GetProjectQueryResult{Result: result[0]}, nil
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

	results := common.NewProjectResultFromModel(matching...)

	if err := s.withBookedHours(ctx, q.ViewerID, results); err != nil {
		return nil, err
	}

	return &query.ListProjectsQueryResult{
		Result:     results,
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

	if err := domainservice.RequireVisible(existingProject, cmd.ActorID); err != nil {
		return nil, err
	}

	if cmd.Name != nil {
		existingProject.Name = *cmd.Name
	}

	// An emptied description is no description rather than an empty one. The
	// difference reaches the screen: the table writes a dash where a project has
	// none, and an empty string is a value - so the cell came out blank and read
	// as a column that had broken.
	if cmd.Description != nil {
		if *cmd.Description == "" {
			existingProject.Description = nil
		} else {
			existingProject.Description = cmd.Description
		}
	}

	if cmd.StartDate != nil {
		existingProject.StartDate = *cmd.StartDate
	}

	// Clearing wins over setting, and both are only what the caller asked for.
	// Silence changes nothing, which is what every partial update depends on.
	switch {
	case cmd.ClearEndDate:
		existingProject.EndDate = nil
	case cmd.EndDate != nil:
		existingProject.EndDate = cmd.EndDate
	}

	if cmd.Status != nil {
		if err := s.validateStatusTransition(ctx, existingProject, *cmd.Status); err != nil {
			return nil, err
		}

		existingProject.Status = *cmd.Status
	}

	if cmd.BudgetHours != nil {
		if err := validateBudget(cmd.BudgetHours); err != nil {
			return nil, err
		}

		existingProject.BudgetHours = budgetOrNone(cmd.BudgetHours)
	}

	if cmd.Billable != nil {
		existingProject.Billable = *cmd.Billable
	}

	if cmd.HourlyRate != nil {
		if err := validateRate(cmd.HourlyRate); err != nil {
			return nil, err
		}

		existingProject.HourlyRate = hundredths(cmd.HourlyRate)
	}

	if cmd.Currency != nil {
		currency, err := currencyCode(*cmd.Currency)
		if err != nil {
			return nil, err
		}

		existingProject.Currency = currency
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

	result := common.NewProjectResultFromModel(updatedProject)

	if err := s.withBookedHours(ctx, cmd.ActorID, result); err != nil {
		return nil, err
	}

	return &command.UpdateProjectCommandResult{Result: result[0]}, nil
}

// validateBudget refuses a budget that is no number of hours one person spends on
// a project. Inside the range rather than outside it, so NaN is refused: see
// validateTimesheet.
func validateBudget(hours *float64) error {
	if hours != nil && !(*hours >= 0 && *hours <= model.MaxBudgetHours) {
		return apperror.InvalidFields("budgetHours")
	}

	return nil
}

// validateRate refuses a rate below nothing or beyond any that is charged, and one
// that is not a number at all.
func validateRate(rate *float64) error {
	if rate != nil && !(*rate >= 0 && *rate <= model.MaxHourlyRate) {
		return apperror.InvalidFields("hourlyRate")
	}

	return nil
}

// hundredths keeps a rate as the hundredths of its currency it is charged in, and
// a rate of nothing as no rate, which is what it means.
func hundredths(rate *float64) *int64 {
	if rate == nil {
		return nil
	}

	value := int64(math.Round(*rate * 100))
	if value == 0 {
		return nil
	}

	return &value
}

// currencyCode reads a currency as its ISO 4217 code: three letters, in capitals.
// Empty is the default one; anything else is refused rather than guessed at,
// because a statement in the wrong currency is wrong by the whole exchange rate.
func currencyCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return model.DefaultCurrency, nil
	}

	if len(code) != 3 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return "", apperror.InvalidFields("currency")
	}

	return code, nil
}

// budgetOrNone stores a budget of nothing as no budget, which is what it means.
func budgetOrNone(hours *float64) *float64 {
	if hours == nil || *hours == 0 {
		return nil
	}

	return hours
}

// withBookedHours fills in how many of the viewer's own hours are booked on each
// project - theirs alone, for the reason the project report gives: nobody sees
// what anybody else has.
func (s *ProjectApplicationService) withBookedHours(
	ctx context.Context,
	viewerID uint,
	results []*common.ProjectResult,
) error {
	totals, err := s.timesheetRepository.HoursByProject(ctx, viewerID)
	if err != nil {
		return err
	}

	for _, result := range results {
		result.BookedHours = settled(totals[result.ID])
	}

	return nil
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

	if err := domainservice.RequireVisible(project, cmd.ActorID); err != nil {
		return err
	}

	// Counted rather than loaded: the refusal needs the number and nothing else.
	entries, err := s.timesheetRepository.CountByFilter(ctx, repository.TimesheetFilter{ProjectID: cmd.ID})
	if err != nil {
		return err
	}

	if entries > 0 {
		return apperror.Conflictf("cannot delete a project that still has %d time entries", entries).
			WithCode("projectHasEntries", entries)
	}

	// A clock running against it counts too, for the same reason and one more.
	// running_timers.project_id is a foreign key with no ON DELETE behaviour: the
	// delete is refused outright by PostgreSQL, and accepted by SQLite and MySQL,
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

	if !model.OnTheCalendar(startDate) {
		invalid = append(invalid, "startDate")
	}

	if endDate != nil && (!model.OnTheCalendar(*endDate) ||
		(!startDate.IsZero() && endDate.Before(startDate))) {
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
