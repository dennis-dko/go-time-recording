package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

func TestDefaultRolesAreSeeded(t *testing.T) {
	f := newFixture(t)

	roles, err := f.roles.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}

	byName := make(map[string]*model.Role, len(roles))
	for _, role := range roles {
		byName[role.Name] = role
	}

	for _, expected := range []string{model.RoleAdmin, model.RoleEmployee} {
		if _, ok := byName[expected]; !ok {
			t.Fatalf("expected default role %q to exist", expected)
		}
	}

	// The admin role must be able to administer, or an installation could end
	// up with nobody able to manage roles.
	if !byName[model.RoleAdmin].Has(model.PermRoleWrite) {
		t.Error("the admin role must hold roles:write")
	}

	// An ordinary account keeps its own time and nobody else's: there is no
	// reviewer any more, and the rights over other people's work are what the
	// administrator was deliberately stripped of.
	for _, forbidden := range []string{
		model.PermTimesheetReadAll, model.PermTimesheetWriteAll,
	} {
		if byName[model.RoleEmployee].Has(forbidden) {
			t.Errorf("an ordinary account must not hold %q", forbidden)
		}
	}

	// And there is only one right that answers "may this person see somebody
	// else's time". reports:read was a second one, for a total rather than a list,
	// and it belonged to the role that reviewed other people's hours. Two rights
	// for one question is how the two come to disagree - and this one had drifted
	// to where no role held it while a whole screen was gated on it.
	for _, permission := range model.AllPermissions() {
		if permission == "reports:read" {
			t.Error(`reports:read is back in AllPermissions(); whether somebody may see ` +
				`another person's time is timesheets:read:all, and one question takes one right`)
		}
	}

	// And it does keep its own projects, which is what replaced the manager.
	for _, own := range []string{
		model.PermProjectWrite, model.PermProjectArchive, model.PermProjectDelete,
	} {
		if !byName[model.RoleEmployee].Has(own) {
			t.Errorf("an ordinary account must hold %q to keep its own projects", own)
		}
	}
}

func TestSystemRoleCannotBeDeletedOrWeakened(t *testing.T) {
	f := newFixture(t)

	admin := roleNamed(t, f, model.RoleAdmin)

	requireKind(t, f.roles.DeleteRole(context.Background(), admin.ID), apperror.KindConflict)

	// Stripping permissions from the system role would lock the installation
	// out of its own administration.
	_, err := f.roles.UpdateRole(context.Background(), admin.ID, nil, nil, []string{model.PermUserRead})
	requireKind(t, err, apperror.KindConflict)

	renamed := "superuser"
	_, err = f.roles.UpdateRole(context.Background(), admin.ID, &renamed, nil, nil)
	requireKind(t, err, apperror.KindConflict)
}

// roleNamed returns the seeded role with the given name.
func roleNamed(t *testing.T, f *fixture, name string) *model.Role {
	t.Helper()

	roles, err := f.roles.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}

	for _, role := range roles {
		if role.Name == name {
			return role
		}
	}

	t.Fatalf("role %q not found", name)

	return nil
}

func TestCreateRoleRejectsUnknownPermission(t *testing.T) {
	f := newFixture(t)

	_, err := f.roles.CreateRole(context.Background(), "auditor", "",
		[]string{model.PermUserRead, "timesheets:invent"})
	requireKind(t, err, apperror.KindInvalid)
}

func TestRoleInUseCannotBeDeleted(t *testing.T) {
	f := newFixture(t)

	role, err := f.roles.CreateRole(context.Background(), "auditor", "read only",
		[]string{model.PermUserRead})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	if _, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Auditor", Email: "auditor@example.com", Role: "auditor",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	requireKind(t, f.roles.DeleteRole(context.Background(), role.ID), apperror.KindConflict)
}

func TestSystemUserIsProtected(t *testing.T) {
	f := newFixture(t)

	created, err := f.auth.EnsureSystemUser(context.Background())
	if err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	if !created {
		t.Fatal("expected the system user to be created on first call")
	}

	// Calling again must not create a second one.
	again, err := f.auth.EnsureSystemUser(context.Background())
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	if again {
		t.Error("the system user must only be created once")
	}

	principal, err := f.auth.Authenticate(context.Background(),
		service.SystemUserEmail, service.SystemUserPassword)
	if err != nil {
		t.Fatalf("authenticate system user: %v", err)
	}

	if !principal.User.IsSystem {
		t.Error("the built-in administrator must be flagged as a system user")
	}

	if !principal.User.MustChangePassword {
		t.Error("the initial password must be flagged for replacement")
	}

	// Deleting it would leave the installation with no guaranteed way back in.
	err = f.users.DeleteUser(context.Background(),
		command.DeleteUserCommand{ID: principal.User.ID})
	requireKind(t, err, apperror.KindConflict)

	// Nor may it be demoted to a role that cannot administer.
	_, err = f.userDomain.AssignRoleToUser(context.Background(),
		principal.User.ID, model.RoleEmployee)
	requireKind(t, err, apperror.KindConflict)
}

func TestAuthenticationRejectsWrongPassword(t *testing.T) {
	f := newFixture(t)

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	if _, err := f.auth.Authenticate(context.Background(),
		service.SystemUserEmail, "not-the-password"); err == nil {
		t.Fatal("expected authentication to fail")
	}

	// An unknown account must fail the same way as a wrong password, so the
	// response cannot be used to discover which addresses exist.
	_, unknownErr := f.auth.Authenticate(context.Background(), "nobody@example.com", "whatever")
	_, wrongErr := f.auth.Authenticate(context.Background(), service.SystemUserEmail, "wrong")

	if unknownErr == nil || wrongErr == nil {
		t.Fatal("both cases must fail")
	}

	if unknownErr.Error() != wrongErr.Error() {
		t.Errorf("failures must be indistinguishable: %q vs %q", unknownErr, wrongErr)
	}
}

func TestChangePassword(t *testing.T) {
	f := newFixture(t)

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	principal, err := f.auth.Authenticate(context.Background(),
		service.SystemUserEmail, service.SystemUserPassword)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	// Too short.
	err = f.auth.ChangePassword(context.Background(), principal.User.ID,
		service.SystemUserPassword, "short")
	requireKind(t, err, apperror.KindInvalid)

	// Wrong current password.
	err = f.auth.ChangePassword(context.Background(), principal.User.ID,
		"wrong-current", "a-much-better-password")
	requireKind(t, err, apperror.KindInvalid)

	if err := f.auth.ChangePassword(context.Background(), principal.User.ID,
		service.SystemUserPassword, "a-much-better-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := f.auth.Authenticate(context.Background(),
		service.SystemUserEmail, service.SystemUserPassword); err == nil {
		t.Error("the old password must stop working")
	}

	updated, err := f.auth.Authenticate(context.Background(),
		service.SystemUserEmail, "a-much-better-password")
	if err != nil {
		t.Fatalf("authenticate with new password: %v", err)
	}

	if updated.User.MustChangePassword {
		t.Error("changing the password must clear the must-change flag")
	}
}

func TestPrincipalPermissionsComeFromRole(t *testing.T) {
	f := newFixture(t)

	if _, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Worker", Email: "worker@example.com", Role: model.RoleEmployee,
		Password: "worker-password",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	principal, err := f.auth.Authenticate(context.Background(),
		"worker@example.com", "worker-password")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if !principal.Can(model.PermTimesheetWriteOwn) {
		t.Error("an employee must be able to book their own time")
	}

	if principal.Can(model.PermUserDelete) {
		t.Error("an employee must not be able to delete users")
	}
}
