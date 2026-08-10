package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/query"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

const maxDailyHours = 10

// fixture wires the services against in-memory repositories and seeds one
// user and one active project, which nearly every case needs.
type fixture struct {
	timesheets *service.TimesheetApplicationService
	projects   *service.ProjectApplicationService
	users      *service.UserApplicationService

	roles *service.RoleApplicationService
	auth  *service.AuthService

	// domain services, exercised where a rule lives in the domain layer
	tsDomain      *domainservice.TimesheetDomainService
	projectDomain *domainservice.ProjectDomainService
	userDomain    *domainservice.UserDomainService

	// repositories, so a test can build a service the fixture does not wire
	userRepo      *memory.UserRepository
	roleRepo      *memory.RoleRepository
	timesheetRepo *memory.TimesheetRepository

	userID    uint
	projectID uint
}

// memoryPurger stands in for the SQL purge in the unit tests: it removes an
// account's time entries and then the account.
//
// Deliberately not a no-op stub. The service decides whether a deletion is
// allowed and what it takes with it, and a purger that removed nothing would let
// a test pass while the account's hours survived. Whether the *real* cascade
// covers every table is a question about SQL, and the integration tests ask it
// against all three databases.
type memoryPurger struct {
	users      repository.UserRepository
	timesheets repository.TimesheetRepository
}

func (p *memoryPurger) PurgeUser(ctx context.Context, userID uint) error {
	entries, err := p.timesheets.GetByFilter(ctx, repository.TimesheetFilter{UserID: userID})
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := p.timesheets.Delete(ctx, entry.ID); err != nil {
			return err
		}
	}

	return p.users.Delete(ctx, userID)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	userRepo := memory.NewUserRepository()
	roleRepo := memory.NewRoleRepository(userRepo)
	projectRepo := memory.NewProjectRepository()
	timesheetRepo := memory.NewTimesheetRepository()

	f := &fixture{
		users:         service.NewUserApplicationService(userRepo, roleRepo, timesheetRepo, &memoryPurger{users: userRepo, timesheets: timesheetRepo}),
		roles:         service.NewRoleApplicationService(roleRepo),
		auth:          service.NewAuthService(userRepo, roleRepo),
		projects:      service.NewProjectApplicationService(projectRepo, timesheetRepo, memory.NewTimerRepository()),
		timesheets:    service.NewTimesheetApplicationService(timesheetRepo, userRepo, projectRepo, maxDailyHours),
		tsDomain:      domainservice.NewTimesheetDomainService(timesheetRepo, projectRepo, userRepo),
		projectDomain: domainservice.NewProjectDomainService(projectRepo, timesheetRepo),
		userDomain:    domainservice.NewUserDomainService(userRepo, roleRepo),
		userRepo:      userRepo,
		roleRepo:      roleRepo,
		timesheetRepo: timesheetRepo,
	}

	user, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Dennis", Email: "dennis@example.com", Role: model.UserRoleEmployee,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	f.userID = user.Result.ID

	// Owned, because every project is: one without an owner is a row nobody can see,
	// which is what the fixture's project used to be when there were shared ones.
	project, err := f.projects.CreateProject(context.Background(), command.CreateProjectCommand{
		Name: "Website", StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		OwnerID: &f.userID,
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}

	f.projectID = project.Result.ID

	return f
}

func (f *fixture) book(t *testing.T, day time.Time, hours float64) uint {
	t.Helper()

	res, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: f.projectID, Date: day, DurationHours: hours,
	})
	if err != nil {
		t.Fatalf("book %.2fh: %v", hours, err)
	}

	return res.Result.ID
}

func day(d int) time.Time {
	return time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC)
}

func requireKind(t *testing.T, err error, want apperror.Kind) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected an error of kind %v, got nil", want)
	}

	if got := apperror.KindOf(err); got != want {
		t.Fatalf("expected kind %v, got %v (%v)", want, got, err)
	}
}

func TestCreateTimesheetRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)

	cases := map[string]command.CreateTimesheetCommand{
		"zero hours":    {UserID: f.userID, ProjectID: f.projectID, Date: day(15), DurationHours: 0},
		"over 24 hours": {UserID: f.userID, ProjectID: f.projectID, Date: day(15), DurationHours: 25},
		"missing date":  {UserID: f.userID, ProjectID: f.projectID, DurationHours: 4},
	}

	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.timesheets.CreateTimesheet(context.Background(), cmd)
			requireKind(t, err, apperror.KindInvalid)
		})
	}
}

func TestCreateTimesheetRejectsUnknownUserOrProject(t *testing.T) {
	f := newFixture(t)

	_, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: 999, ProjectID: f.projectID, Date: day(15), DurationHours: 4,
	})
	requireKind(t, err, apperror.KindNotFound)

	_, err = f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: 999, Date: day(15), DurationHours: 4,
	})
	requireKind(t, err, apperror.KindNotFound)
}

func TestDailyHoursCap(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 8)

	// 8h + 3h exceeds the 10h cap for that day.
	_, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: f.projectID, Date: day(15), DurationHours: 3,
	})
	requireKind(t, err, apperror.KindConflict)

	// The same booking on the next day is fine, proving the cap is per-day.
	if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: f.projectID, Date: day(16), DurationHours: 3,
	}); err != nil {
		t.Fatalf("next day should not be capped: %v", err)
	}
}

// An update must not count the entry it is updating towards the daily cap,
// otherwise raising 8h to 9h would look like 17h.
func TestDailyHoursCapExcludesTheEntryBeingUpdated(t *testing.T) {
	f := newFixture(t)
	id := f.book(t, day(15), 8)

	hours := 9.0
	if _, err := f.timesheets.UpdateTimesheet(context.Background(), command.UpdateTimesheetCommand{
		ID: id, DurationHours: &hours,
	}); err != nil {
		t.Fatalf("raising 8h to 9h under a 10h cap should succeed: %v", err)
	}
}

func TestListTimesheetsFiltersByUser(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 4)

	other, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Other", Email: "other@example.com", Role: model.UserRoleEmployee,
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	res, err := f.timesheets.ListTimesheets(context.Background(),
		query.ListTimesheetsQuery{UserID: other.Result.ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if res.TotalCount != 0 {
		t.Errorf("expected no entries for the second user, got %d", res.TotalCount)
	}
}

func TestBookingOntoNonActiveProjectIsRejected(t *testing.T) {
	f := newFixture(t)

	completed := model.ProjectStatusCompleted
	if _, err := f.projects.UpdateProject(context.Background(),
		command.UpdateProjectCommand{ID: f.projectID, Status: &completed}); err != nil {
		t.Fatalf("completing project: %v", err)
	}

	_, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, ProjectID: f.projectID, Date: day(15), DurationHours: 4,
	})
	requireKind(t, err, apperror.KindConflict)
}

// Hours may be recorded before it is known which project they belong to, so a
// booking without a project must succeed and stay uncategorised.
func TestBookingWithoutProjectIsAllowed(t *testing.T) {
	f := newFixture(t)

	res, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, Date: day(15), DurationHours: 4,
	})
	if err != nil {
		t.Fatalf("booking without a project: %v", err)
	}

	if res.Result.ProjectID != nil {
		t.Errorf("expected no project, got %v", *res.Result.ProjectID)
	}
}

// Assigning a project afterwards is how an entry gets categorised.
func TestUncategorisedEntryCanBeAssignedLater(t *testing.T) {
	f := newFixture(t)

	created, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, Date: day(15), DurationHours: 4,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	projectID := f.projectID
	updated, err := f.timesheets.UpdateTimesheet(context.Background(), command.UpdateTimesheetCommand{
		ID: created.Result.ID, ProjectID: &projectID,
	})
	if err != nil {
		t.Fatalf("assign project: %v", err)
	}

	if updated.Result.ProjectID == nil || *updated.Result.ProjectID != projectID {
		t.Errorf("expected project %d, got %v", projectID, updated.Result.ProjectID)
	}

	// And it can be removed again by sending the zero id.
	var none uint
	cleared, err := f.timesheets.UpdateTimesheet(context.Background(), command.UpdateTimesheetCommand{
		ID: created.Result.ID, ProjectID: &none,
	})
	if err != nil {
		t.Fatalf("clear project: %v", err)
	}

	if cleared.Result.ProjectID != nil {
		t.Error("expected the project assignment to be removed")
	}
}

// The filter must be able to find exactly the uncategorised entries.
func TestFilterFindsEntriesWithoutProject(t *testing.T) {
	f := newFixture(t)
	f.book(t, day(15), 4) // with project

	if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
		UserID: f.userID, Date: day(16), DurationHours: 2,
	}); err != nil {
		t.Fatalf("create uncategorised: %v", err)
	}

	found, err := f.timesheetRepo.GetByFilter(context.Background(),
		repository.TimesheetFilter{WithoutProject: true})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	if len(found) != 1 {
		t.Fatalf("expected 1 uncategorised entry, got %d", len(found))
	}

	if found[0].HasProject() {
		t.Error("the returned entry must have no project")
	}
}
