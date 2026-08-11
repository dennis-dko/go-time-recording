package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// These three operations are the ones with real refusals in them, and the
// refusals are the point: archiving a project with open entries would strand
// them, transferring an approved entry would rewrite a total somebody has
// already reported, and moving the built-in administrator to a role without
// administration rights would lock an installation out of its own user
// management.
//
// Each is reachable through the API, so each is something a caller can try.

type fixture struct {
	users      repository.UserRepository
	roles      repository.RoleRepository
	projects   repository.ProjectRepository
	timesheets repository.TimesheetRepository

	userDomain      *service.UserDomainService
	projectDomain   *service.ProjectDomainService
	timesheetDomain *service.TimesheetDomainService
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	users := memory.NewUserRepository()
	roles := memory.NewRoleRepository(users)
	projects := memory.NewProjectRepository()
	timesheets := memory.NewTimesheetRepository()

	return &fixture{
		users:           users,
		roles:           roles,
		projects:        projects,
		timesheets:      timesheets,
		userDomain:      service.NewUserDomainService(users, roles),
		projectDomain:   service.NewProjectDomainService(projects, timesheets),
		timesheetDomain: service.NewTimesheetDomainService(timesheets, projects, users),
	}
}

func (f *fixture) project(t *testing.T, name, status string) *model.Project {
	t.Helper()

	created, err := f.projects.Save(context.Background(), &model.Project{Name: name, Status: status})
	if err != nil {
		t.Fatalf("seed project %s: %v", name, err)
	}

	return created
}

func (f *fixture) entry(t *testing.T, projectID uint) *model.Timesheet {
	t.Helper()

	entry := &model.Timesheet{
		UserID: 1, Date: time.Now(), DurationHours: 3,
	}

	if projectID != 0 {
		entry.ProjectID = &projectID
	}

	created, err := f.timesheets.Save(context.Background(), entry)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	return created
}

// role returns the named role, creating it only if the repository does not
// already have one.
//
// The memory repository seeds the standard roles, and saving a second "user"
// would leave two - with GetByName answering the first, so the test would compare
// against a role nothing points at.
func (f *fixture) role(t *testing.T, name string, permissions ...string) *model.Role {
	t.Helper()

	if existing, err := f.roles.GetByName(context.Background(), name); err == nil && existing != nil {
		return existing
	}

	created, err := f.roles.Save(context.Background(),
		&model.Role{Name: name, Permissions: permissions})
	if err != nil {
		t.Fatalf("seed role %s: %v", name, err)
	}

	return created
}

func (f *fixture) user(t *testing.T, email string, roleID uint, system bool) *model.User {
	t.Helper()

	created, err := f.users.Save(context.Background(), &model.User{
		Name: email, Email: email, RoleID: roleID, IsSystem: system,
	})
	if err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}

	return created
}

// --------------------------------------------------------------- archiving

func TestArchivingACompletedProjectWithNoOpenEntries(t *testing.T) {
	f := newFixture(t)
	project := f.project(t, "Finished", model.ProjectStatusCompleted)

	archived, err := f.projectDomain.ArchiveProject(context.Background(), project.ID, 0)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	if archived.Status != model.ProjectStatusArchived {
		t.Errorf("status is %q, want %q", archived.Status, model.ProjectStatusArchived)
	}
}

// Archiving an active project would hide something people are still booking on.
func TestOnlyACompletedProjectCanBeArchived(t *testing.T) {
	for _, status := range []string{model.ProjectStatusActive, model.ProjectStatusArchived} {
		f := newFixture(t)
		project := f.project(t, "Ongoing", status)

		if _, err := f.projectDomain.ArchiveProject(context.Background(), project.ID, 0); err == nil {
			t.Errorf("a %q project was archived", status)
		}
	}
}

// Entries are no obstacle - a finished project has them by definition, and
// refusing here would make archiving impossible in practice.
func TestEntriesDoNotBlockArchiving(t *testing.T) {
	f := newFixture(t)
	project := f.project(t, "Done", model.ProjectStatusCompleted)
	f.entry(t, project.ID)
	f.entry(t, project.ID)

	if _, err := f.projectDomain.ArchiveProject(context.Background(), project.ID, 0); err != nil {
		t.Errorf("archiving was refused over settled entries: %v", err)
	}
}

func TestArchivingAProjectThatIsNotThere(t *testing.T) {
	f := newFixture(t)

	if _, err := f.projectDomain.ArchiveProject(context.Background(), 9999, 0); err == nil {
		t.Error("archiving a project that does not exist succeeded")
	}
}

// -------------------------------------------------------------- transfers

func TestTransferringAnEntryToAnotherProject(t *testing.T) {
	f := newFixture(t)
	from := f.project(t, "From", model.ProjectStatusActive)
	to := f.project(t, "To", model.ProjectStatusActive)
	entry := f.entry(t, from.ID)

	moved, err := f.timesheetDomain.TransferTimesheetToProject(context.Background(), entry.ID, to.ID, 0)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if moved.ProjectID == nil || *moved.ProjectID != to.ID {
		t.Errorf("the entry is on %v, want project %d", moved.ProjectID, to.ID)
	}
}

// Transferring is also how an entry booked without a project gets its first
// one, which is the reason the operation is not called "move".
func TestAnEntryWithNoProjectCanBeGivenOne(t *testing.T) {
	f := newFixture(t)
	target := f.project(t, "Target", model.ProjectStatusActive)
	entry := f.entry(t, 0)

	moved, err := f.timesheetDomain.TransferTimesheetToProject(context.Background(), entry.ID, target.ID, 0)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if moved.ProjectID == nil || *moved.ProjectID != target.ID {
		t.Errorf("the entry is on %v, want project %d", moved.ProjectID, target.ID)
	}
}

// A project that no longer accepts time must not receive any by the back door.
func TestAnEntryCannotBeTransferredIntoAClosedProject(t *testing.T) {
	for _, status := range []string{model.ProjectStatusCompleted, model.ProjectStatusArchived} {
		f := newFixture(t)
		from := f.project(t, "From", model.ProjectStatusActive)
		to := f.project(t, "Closed", status)
		entry := f.entry(t, from.ID)

		if _, err := f.timesheetDomain.TransferTimesheetToProject(
			context.Background(), entry.ID, to.ID, 0); err == nil {
			t.Errorf("an entry was transferred into a %q project", status)
		}
	}
}

// Refused rather than silently accepted, so a double-click does not read as
// success and leave the caller unsure what happened.
func TestTransferringAnEntryToTheProjectItIsAlreadyOn(t *testing.T) {
	f := newFixture(t)
	project := f.project(t, "Here", model.ProjectStatusActive)
	entry := f.entry(t, project.ID)

	if _, err := f.timesheetDomain.TransferTimesheetToProject(
		context.Background(), entry.ID, project.ID, 0); err == nil {
		t.Error("transferring an entry onto its own project succeeded")
	}
}

func TestTransferringToAProjectThatIsNotThere(t *testing.T) {
	f := newFixture(t)
	from := f.project(t, "From", model.ProjectStatusActive)
	entry := f.entry(t, from.ID)

	if _, err := f.timesheetDomain.TransferTimesheetToProject(
		context.Background(), entry.ID, 9999, 0); err == nil {
		t.Error("transferring to a project that does not exist succeeded")
	}
}

// ---------------------------------------------------------------- reports

func TestTheReportTotalsHoursPerPerson(t *testing.T) {
	f := newFixture(t)
	project := f.project(t, "Reported", model.ProjectStatusActive)

	seedFor := func(userID uint, hours float64, daysAgo int) {
		t.Helper()

		id := project.ID
		_, err := f.timesheets.Save(context.Background(), &model.Timesheet{
			UserID: userID, ProjectID: &id, DurationHours: hours,
			Date: time.Now().AddDate(0, 0, -daysAgo),
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	seedFor(1, 3, 2)
	seedFor(1, 2.5, 1)
	seedFor(2, 4, 1)

	// The trailing zero is "everybody": what a caller who may read everyone's time
	// gets. The case below covers the narrowing that everybody else gets.
	report, err := f.timesheetDomain.GenerateProjectTimeReport(context.Background(),
		project.ID, time.Now().AddDate(0, 0, -7), time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	if report[1] != 5.5 {
		t.Errorf("user 1 has %v hours, want 5.5", report[1])
	}

	if report[2] != 4 {
		t.Errorf("user 2 has %v hours, want 4", report[2])
	}
}

// An empty period is a legitimate answer, not an error - a project can simply
// have had nothing booked on it that month.
func TestAReportOverAPeriodWithNothingInItIsEmpty(t *testing.T) {
	f := newFixture(t)
	project := f.project(t, "Quiet", model.ProjectStatusActive)

	report, err := f.timesheetDomain.GenerateProjectTimeReport(context.Background(),
		project.ID, time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0), 0, 0)
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	if len(report) != 0 {
		t.Errorf("got %v, want nothing", report)
	}
}

func TestAReportForAProjectThatIsNotThere(t *testing.T) {
	f := newFixture(t)

	if _, err := f.timesheetDomain.GenerateProjectTimeReport(context.Background(),
		9999, time.Now().AddDate(0, 0, -7), time.Now(), 0, 0); err == nil {
		t.Error("a report was produced for a project that does not exist")
	}
}

// ------------------------------------------------------------ role changes

func TestAssigningARoleToAnOrdinaryUser(t *testing.T) {
	f := newFixture(t)
	everyday := f.role(t, "user", model.PermTimesheetWriteOwn)

	// Not "manager": there is no such role any more, and no right one could be built
	// from either. A custom role an installation could still create, which is all this
	// case needs - somewhere to move somebody.
	oversight := f.role(t, "oversight", model.PermProjectRead)
	user := f.user(t, "someone@example.com", everyday.ID, false)

	updated, err := f.userDomain.AssignRoleToUser(context.Background(), user.ID, "oversight")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	if updated.RoleID != oversight.ID {
		t.Errorf("the user is on role %d, want %d", updated.RoleID, oversight.ID)
	}
}

// The built-in administrator exists so an installation always has a way back
// in. A role that cannot administer roles is not a way back in - and this is
// reachable over the API, so it is something somebody can try by accident while
// tidying up permissions.
func TestTheBuiltInAdministratorCannotLoseAdministrationRights(t *testing.T) {
	f := newFixture(t)
	admin := f.role(t, "admin", model.AllPermissions()...)
	user := f.role(t, "user", model.PermTimesheetWriteOwn)
	system := f.user(t, "admin@local", admin.ID, true)

	_, err := f.userDomain.AssignRoleToUser(context.Background(), system.ID, "user")
	if err == nil {
		t.Fatal("the built-in administrator was moved to a role that cannot administer")
	}

	// And the refusal has to be an actual refusal, not a rollback after the fact.
	after, readErr := f.users.GetByID(context.Background(), system.ID)
	if readErr != nil {
		t.Fatalf("read the administrator back: %v", readErr)
	}

	if after.RoleID != admin.ID {
		t.Errorf("the role changed to %d despite the refusal", after.RoleID)
	}

	if after.RoleID == user.ID {
		t.Error("the administrator ended up on the user role")
	}
}

// Moving it to a *different* role that can still administer is fine - the rule
// is about the capability, not about pinning it to one particular role.
func TestTheBuiltInAdministratorCanMoveToAnotherAdministeringRole(t *testing.T) {
	f := newFixture(t)
	admin := f.role(t, "admin", model.AllPermissions()...)
	other := f.role(t, "superuser", model.PermRoleWrite, model.PermUserWrite)
	system := f.user(t, "admin@local", admin.ID, true)

	updated, err := f.userDomain.AssignRoleToUser(context.Background(), system.ID, "superuser")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	if updated.RoleID != other.ID {
		t.Errorf("the administrator is on role %d, want %d", updated.RoleID, other.ID)
	}
}

func TestAssigningARoleThatDoesNotExist(t *testing.T) {
	f := newFixture(t)
	everyday := f.role(t, "user", model.PermTimesheetWriteOwn)
	user := f.user(t, "someone@example.com", everyday.ID, false)

	if _, err := f.userDomain.AssignRoleToUser(context.Background(), user.ID, "wizard"); err == nil {
		t.Error("a role that does not exist was assigned")
	}
}

func TestAssigningARoleToAUserThatDoesNotExist(t *testing.T) {
	f := newFixture(t)
	f.role(t, "user", model.PermTimesheetWriteOwn)

	if _, err := f.userDomain.AssignRoleToUser(context.Background(), 9999, "user"); err == nil {
		t.Error("a role was assigned to a user that does not exist")
	}
}
