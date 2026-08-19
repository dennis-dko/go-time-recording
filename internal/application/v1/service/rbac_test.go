package service_test

import (
	"context"
	"slices"
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

	for _, expected := range []string{model.RoleAdmin, model.RoleUser} {
		if _, ok := byName[expected]; !ok {
			t.Fatalf("expected default role %q to exist", expected)
		}
	}

	// The admin role must be able to administer, or an installation could end
	// up with nobody able to manage roles.
	if !byName[model.RoleAdmin].Has(model.PermRoleWrite) {
		t.Error("the admin role must hold roles:write")
	}

	// No right answers "may this person see somebody else's time", because the
	// answer is no and it is not a choice.
	//
	// This used to check that no seeded role held those rights, which was the weaker
	// statement: the rights still existed, so an administrator could tick one in the
	// role editor. That granted a capability with no screen behind it - every
	// question about every colleague answered by the API, and nothing anywhere to
	// show it had been granted.
	//
	// Named as literals rather than constants, because the point is that the
	// constants are gone. A guard written against a constant disappears with it, and
	// disappears silently.
	for _, gone := range []string{
		"timesheets:read:all",
		"timesheets:write:all",
		// A total rather than a list, asked of somebody else's hours. Same question,
		// second answer - which is how two answers come to disagree.
		"reports:read",
		// Reviewing somebody's hours, from when there was a reviewer.
		"timesheets:approve",
	} {
		if slices.Contains(model.AllPermissions(), gone) {
			t.Errorf("%q is back in AllPermissions(); it lets one account read or write "+
				"another's time, and this application has no such account", gone)
		}

		for name, role := range byName {
			if role.Has(gone) {
				t.Errorf("the seeded role %q holds %q", name, gone)
			}
		}
	}

	// And it does keep its own projects, which is what replaced the manager.
	for _, own := range []string{
		model.PermProjectWrite, model.PermProjectArchive, model.PermProjectDelete,
	} {
		if !byName[model.RoleUser].Has(own) {
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

// signIn is the sign-in the application actually performs.
//
// The tests here used to call AuthService.Authenticate, which the application
// never calls: a second implementation of the password check, differing from the
// real one in that it did not refuse a directory-backed account. Testing it
// proved something about code nobody ran, and left the path that does run
// covered only by the integration suite.
func signIn(t *testing.T, f *fixture, email, password string) *service.Principal {
	t.Helper()

	result, err := f.sessions.Login(context.Background(), email, password, "")
	if err != nil {
		t.Fatalf("signing in as %s: %v", email, err)
	}

	return result.Principal
}

// signInIsRefused is signIn for the cases that are meant to be turned away.
func signInIsRefused(t *testing.T, f *fixture, email, password string) {
	t.Helper()

	if _, err := f.sessions.Login(context.Background(), email, password, ""); err == nil {
		t.Fatalf("signing in as %s with %q was accepted", email, password)
	}
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

	principal := signIn(t, f, service.SystemUserEmail, service.SystemUserPassword)

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
		principal.User.ID, model.RoleUser)
	requireKind(t, err, apperror.KindConflict)
}

func TestAuthenticationRejectsWrongPassword(t *testing.T) {
	f := newFixture(t)

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	signInIsRefused(t, f, service.SystemUserEmail, "not-the-password")

	// An unknown account must fail the same way as a wrong password, so the
	// response cannot be used to discover which addresses exist. Both refusals
	// are wanted here rather than only the fact of them, so this asks for them
	// directly instead of going through the helper.
	_, unknownErr := f.sessions.Login(context.Background(), "nobody@example.com", "whatever", "")
	_, wrongErr := f.sessions.Login(context.Background(), service.SystemUserEmail, "wrong", "")

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

	principal := signIn(t, f, service.SystemUserEmail, service.SystemUserPassword)

	// Too short.
	err := f.auth.ChangePassword(context.Background(), principal.User.ID,
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

	signInIsRefused(t, f, service.SystemUserEmail, service.SystemUserPassword)

	updated := signIn(t, f, service.SystemUserEmail, "a-much-better-password")

	if updated.User.MustChangePassword {
		t.Error("changing the password must clear the must-change flag")
	}
}

func TestPrincipalPermissionsComeFromRole(t *testing.T) {
	f := newFixture(t)

	if _, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: "Worker", Email: "worker@example.com", Role: model.RoleUser,
		Password: "worker-password",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	principal := signIn(t, f, "worker@example.com", "worker-password")

	if !principal.Can(model.PermTimesheetWriteOwn) {
		t.Error("a user must be able to book their own time")
	}

	if principal.Can(model.PermUserDelete) {
		t.Error("a user must not be able to delete users")
	}
}
