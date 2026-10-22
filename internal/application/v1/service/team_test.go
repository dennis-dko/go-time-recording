package service_test

import (
	"context"
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// teamFixture is the booking fixture with a manager, two more accounts and the
// managers' service over them.
type teamFixture struct {
	*fixture

	team                   *service.TeamService
	manager, member, other *model.User
	managerAsCaller        *service.Principal
}

func newTeamFixture(t *testing.T) *teamFixture {
	t.Helper()

	f := &teamFixture{fixture: newFixture(t)}
	f.team = service.NewTeamService(memory.NewManagedAccountRepository(), newStubSettings(), f.userRepo, f.roleRepo,
		f.projectRepo)

	make := func(name, email, role string) *model.User {
		created, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
			Name: name, Email: email, Role: role,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}

		user, err := f.userRepo.GetByID(context.Background(), created.Result.ID)
		if err != nil {
			t.Fatalf("read %s back: %v", name, err)
		}

		return user
	}

	f.manager = make("Anna", "anna@example.com", model.RoleManager)
	f.member = make("Ben", "ben@example.com", model.RoleUser)
	f.other = make("Clara", "clara@example.com", model.RoleUser)
	f.managerAsCaller = &service.Principal{User: f.manager, Permissions: model.ManagerPermissions()}

	return f
}

func (f *teamFixture) reaches(t *testing.T, caller *service.Principal, userID uint, permission string) bool {
	t.Helper()

	reached, err := f.team.Reaches(context.Background(), caller, userID, permission)
	if err != nil {
		t.Fatalf("reaches: %v", err)
	}

	return reached
}

// A manager reaches an account only while the installation has managers, only one
// an administrator assigned to them, and only with the right in question - and
// never themselves through a team right, which is the own rights' business.
func TestAManagerReachesOnlyTheAssignedAccountsAndOnlyWhileSwitchedOn(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()

	if err := f.team.SetManagers(ctx, f.member.ID, []uint{f.manager.ID}); err != nil {
		t.Fatalf("assign Ben to Anna: %v", err)
	}

	if f.reaches(t, f.managerAsCaller, f.member.ID, model.PermTimesheetReadTeam) {
		t.Error("with the switch off, the manager reaches the account assigned to her")
	}

	if err := f.team.SetEnabled(ctx, true); err != nil {
		t.Fatalf("switch managers on: %v", err)
	}

	if !f.reaches(t, f.managerAsCaller, f.member.ID, model.PermTimesheetReadTeam) {
		t.Error("with the switch on, the manager does not reach the account assigned to her")
	}

	if f.reaches(t, f.managerAsCaller, f.other.ID, model.PermTimesheetReadTeam) {
		t.Error("the manager reaches an account nobody assigned to her")
	}

	readOnly := &service.Principal{User: f.manager,
		Permissions: append(model.UserPermissions(), model.PermTimesheetReadTeam)}
	if f.reaches(t, readOnly, f.member.ID, model.PermTimesheetWriteTeam) {
		t.Error("a manager whose role does not write for the team reaches the account to write")
	}

	if f.reaches(t, f.managerAsCaller, f.manager.ID, model.PermTimesheetReadTeam) {
		t.Error("the manager reaches herself through a team right")
	}

	if f.reaches(t, f.managerAsCaller, f.member.ID, model.PermTimesheetReadOwn) {
		t.Error("an own right is asked as a team right")
	}
}

// The team a manager sees is the accounts assigned to her with the rights her role
// gives her over them, and the people assigned see the same from their side - and
// both are empty while the installation has no managers.
func TestAManagersTeamAndAnAccountsManagersReadTheSameAssignment(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()

	if err := f.team.SetManagers(ctx, f.member.ID, []uint{f.manager.ID}); err != nil {
		t.Fatalf("assign Ben to Anna: %v", err)
	}

	team, err := f.team.Team(ctx, f.managerAsCaller)
	if err != nil || len(team) != 0 {
		t.Fatalf("with the switch off the team is %+v (%v), want nobody", team, err)
	}

	if err := f.team.SetEnabled(ctx, true); err != nil {
		t.Fatalf("switch managers on: %v", err)
	}

	team, err = f.team.Team(ctx, f.managerAsCaller)
	if err != nil {
		t.Fatalf("team: %v", err)
	}

	if len(team) != 1 || team[0].ID != f.member.ID || team[0].Name != "Ben" ||
		!slices.Equal(team[0].Rights, model.TeamPermissions()) {
		t.Errorf("the team reads %+v, want Ben with all three rights", team)
	}

	managers, err := f.team.ManagersOf(ctx, f.member.ID)
	if err != nil {
		t.Fatalf("managers: %v", err)
	}

	if len(managers) != 1 || managers[0].ID != f.manager.ID || managers[0].Name != "Anna" ||
		!slices.Equal(managers[0].Rights, model.TeamPermissions()) {
		t.Errorf("Ben's managers read %+v, want Anna with all three rights", managers)
	}
}

// Only somebody whose role gives a team right can be assigned as a manager, nobody
// manages their own account, and an account that does not exist is not found.
func TestAnAssignmentNamesRealManagersOfSomebodyElse(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		userID   uint
		managers []uint
		code     string
	}{
		{"a manager of herself", f.manager.ID, []uint{f.manager.ID}, "selfManaged"},
		{"somebody without a team right", f.member.ID, []uint{f.other.ID}, "notAManager"},
		{"an account that does not exist", 999, []uint{f.manager.ID}, ""},
	} {
		err := f.team.SetManagers(ctx, tc.userID, tc.managers)

		detail, ok := apperror.Detail(err)

		switch {
		case !ok:
			t.Errorf("%s was accepted", tc.name)
		case tc.code == "" && detail.Kind != apperror.KindNotFound:
			t.Errorf("%s answered %v, want not found", tc.name, err)
		case tc.code != "" && detail.Code != tc.code:
			t.Errorf("%s answered %v, want %s", tc.name, err, tc.code)
		}
	}
}
