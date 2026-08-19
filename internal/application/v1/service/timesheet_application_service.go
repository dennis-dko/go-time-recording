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

	metrics
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
	if err := validateTimesheet(cmd.Date, cmd.DurationHours, cmd.Description); err != nil {
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
	})
	if err != nil {
		return nil, err
	}

	// After the write, so the number counts hours that are actually recorded
	// rather than hours somebody tried to record.
	s.record(ctx, MetricHoursBooked, createdTimesheet.DurationHours)

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
		UserID:         q.UserID,
		ProjectID:      q.ProjectID,
		WithoutProject: q.WithoutProject,
		StartDate:      q.StartDate,
		EndDate:        q.EndDate,
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

	// As it stands, so the checks below can tell an edit that moves the entry from
	// one that leaves it where it is.
	owner, before := existingTimesheet.UserID, existingTimesheet.ProjectID

	if cmd.UserID != nil {
		if _, err := s.userRepository.GetByID(ctx, *cmd.UserID); err != nil {
			return nil, err
		}

		existingTimesheet.UserID = *cmd.UserID
	}

	// Whether this edit moves the entry, and where to. Applied below rather than
	// here, because the check that follows needs the owner to have settled first.
	movedOwner := cmd.UserID != nil && *cmd.UserID != owner

	if cmd.ProjectID != nil {
		// 0 is how a client asks to remove the assignment again, which is what
		// makes an entry uncategorised.
		existingTimesheet.ProjectID = normalizeProjectID(*cmd.ProjectID)
	}

	// The same pairing check booking goes through, for the same reasons: the
	// project has to exist, be one this person may see at all, and still accept
	// hours. Editing checked only that it existed, so an edit could put hours into
	// a colleague's private category - a project the API refuses even to admit
	// exists - or onto one that had been completed, both of which the booking form
	// refuses.
	//
	// Only when something actually moves. An entry already sitting on a completed
	// project has to stay editable: the rule is about moving hours onto a closed
	// project, not about fixing a typo in the description of one. A changed owner
	// counts as a move too, because visibility is a fact about the pair.
	movedProject := cmd.ProjectID != nil &&
		!sameProject(existingTimesheet.ProjectID, before)

	if (movedOwner || movedProject) && existingTimesheet.HasProject() {
		err = s.requireUserAndProject(ctx, existingTimesheet.UserID, *existingTimesheet.ProjectID)
		if err != nil {
			return nil, err
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

	err = validateTimesheet(existingTimesheet.Date, existingTimesheet.DurationHours,
		existingTimesheet.Description)
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

	// Read first, so a delete of something that is not there answers "not found"
	// rather than reporting success for a row nobody had.
	if _, err := s.timesheetRepository.GetByID(ctx, cmd.ID); err != nil {
		return err
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
			project.Name, project.Status).
			WithCode("projectClosedForBooking", project.Name, project.Status)
	}

	return nil
}

// sameProject reports whether two optional project assignments are the same one,
// counting "no project" as equal to itself.
func sameProject(a, b *uint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
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
	limit, err := s.dailyLimitFor(ctx, userID)
	if err != nil {
		return err
	}

	if limit <= 0 {
		return nil
	}

	from := startOfDay(day)
	to := from.AddDate(0, 0, 1).Add(-time.Nanosecond)

	sameDay, filterErr := s.timesheetRepository.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &from,
		EndDate:   &to,
	})
	if filterErr != nil {
		return filterErr
	}

	total := hours

	for _, entry := range sameDay {
		if entry.ID == excludeID {
			continue
		}

		total += entry.DurationHours
	}

	// Compared with room for what the addition itself introduces.
	//
	// Hours are a double, and a sum of them is not the decimal figure a person
	// typed. 0.56 + 6.98 + 0.46 is exactly eight hours on paper and
	// 8.00000000000000177636 here, so a strict comparison refused the third
	// booking of an eight hour day and explained that 8.00h is over the 8.00h
	// limit - a sentence that is wrong on its face and impossible to act on.
	//
	// The slack is a millionth of a second of work. Nothing anybody records is
	// that small, and nothing float64 gets wrong by is that large, so this can
	// only ever forgive the arithmetic.
	if total > limit+roundingSlack {
		return apperror.Conflictf("booking %.2fh would total %.2fh on %s, over the %.2fh daily limit",
			hours, total, from.Format(time.DateOnly), limit).
			WithCode("overDailyLimit", hours, total, from.Format(time.DateOnly), limit)
	}

	return nil
}

// roundingSlack is what a sum of hours may exceed a limit by before it counts as
// exceeding it: a millionth of a second, expressed in hours.
//
// Far above what adding doubles gets wrong, which is around 1e-15, and far below
// anything a person books. See checkDailyLimit for the day this cost somebody.
const roundingSlack = 1e-9

func validateTimesheet(date time.Time, hours float64, description *string) error {
	var invalid []string

	if date.IsZero() {
		invalid = append(invalid, "date")
	}

	// Any duration that was actually worked, to the minute or finer: nothing here
	// rounds to a quarter of an hour, and nothing should - the column is a double
	// and every sum along the way is plain addition.
	//
	// The floor is the one the OpenAPI document publishes rather than a bare
	// "greater than zero", so the form, the document and this check agree. Below
	// it a booking is not a short entry, it is a mistyped one.
	if hours < model.MinBookableHours || hours > model.HoursPerDay {
		invalid = append(invalid, "durationHours")
	}

	// The column is TEXT and would take anything; a description large enough to
	// slow every listing that renders it is still not one.
	if description != nil && model.TooLong(*description, model.MaxDescriptionLength) {
		invalid = append(invalid, "description")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	return nil
}

// dailyCap is the administered booking limit, or the one the environment set.
// dailyLimitFor is how many hours this person may book on one day.
//
// The stricter of two numbers, and both are meant: the installation's ceiling is
// configuration, which the administrator owns, and the personal one is a time figure,
// which belongs to whoever it is about. Taking the lower lets somebody hold their own
// day shorter than the installation allows without letting them raise it past what the
// installation allows - which would be overriding a setting that is not theirs.
//
// The personal figure was stored, offered on screen and consulted by nothing: only the
// instance ceiling was ever read here. A field that changes nothing when you fill it in
// is worse than a field that is not there.
func (s *TimesheetApplicationService) dailyLimitFor(
	ctx context.Context,
	userID uint,
) (float64, error) {
	instance := s.dailyCap(ctx)

	user, err := s.userRepository.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}

	personal := user.MaxDailyHours
	if personal <= 0 {
		return instance, nil
	}

	if instance <= 0 || personal < instance {
		return personal, nil
	}

	return instance, nil
}

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

// WithMetrics attaches the recorder. Optional: without it the service works and
// records nothing, which is what every unit test wants.
func (s *TimesheetApplicationService) WithMetrics(recorder Recorder) *TimesheetApplicationService {
	s.recorder = recorder

	return s
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
