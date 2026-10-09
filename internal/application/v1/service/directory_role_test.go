package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// These tests guard one setting: the role an account the directory brings in
// starts with.
//
// The directory settings offer it as "Default role for new users", check it
// against the roles that exist and store it - and neither way an account arrives
// read it. A first sign-in and a synchronisation were both handed the everyday
// role when the application started, so whatever an administrator chose, every
// account the directory created started as an ordinary user. The screen said one
// thing and the server did another, and the migration that renamed the roles had
// carefully rewritten the stored name of a value nothing read.

// roleOf is the name of the role an account holds.
func roleOf(t *testing.T, f *fixture, email string) string {
	t.Helper()

	user, err := f.userRepo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("no account for %s: %v", email, err)
	}

	role, err := f.roleRepo.GetByID(context.Background(), user.RoleID)
	if err != nil {
		t.Fatalf("the role of %s cannot be read: %v", email, err)
	}

	return role.Name
}

// customRole adds a role of the installation's own, holding the given rights.
func customRole(t *testing.T, f *fixture, name string, permissions ...string) {
	t.Helper()

	if _, err := f.roleRepo.Save(context.Background(), &model.Role{
		Name: name, Permissions: permissions,
	}); err != nil {
		t.Fatalf("create role %s: %v", name, err)
	}
}

func TestAFirstSignInStartsWithTheDirectorysDefaultRole(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "new.person@example.com", Name: "New Person", Role: "contractor",
	})
	customRole(t, f, "contractor", model.PermTimesheetReadOwn, model.PermTimesheetWriteOwn)

	if _, err := sessions.Login(context.Background(), "new.person@example.com", "anything", ""); err != nil {
		t.Fatalf("login: %v", err)
	}

	if got := roleOf(t, f, "new.person@example.com"); got != "contractor" {
		t.Errorf("the account starts as %q, want the directory's default role %q", got, "contractor")
	}
}

func TestASynchronisationCreatesAccountsWithTheDirectorysDefaultRole(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	customRole(t, f.fixture, "contractor", model.PermTimesheetReadOwn, model.PermTimesheetWriteOwn)

	f.directory.users = []service.ExternalUser{
		{ID: "uuid-1", Email: "arrived@example.com", Role: "contractor"},
	}

	if _, err := f.sync.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := roleOf(t, f.fixture, "arrived@example.com"); got != "contractor" {
		t.Errorf("the account starts as %q, want the directory's default role %q", got, "contractor")
	}
}

// A default role that administers the installation is not handed out, whenever
// it came to administer.
//
// Sign-in already refuses to let the directory claim an account that administers
// - whoever can write an entry to the directory could name an administrator's
// address - and a default role that administers would open the same door wider:
// every entry anybody adds would arrive holding the installation. The settings
// refuse to store one; this is the other half, for a role given settings:manage
// or roles:write after it was chosen, and the account starts as an ordinary user.
func TestADefaultRoleThatAdministersIsNotHandedOut(t *testing.T) {
	for _, right := range []string{model.PermSettingsManage, model.PermRoleWrite} {
		f, sessions := newSessionFixture(t, &service.ExternalUser{
			ID: "uuid-1", Email: "new.person@example.com", Role: "keyholder",
		})
		customRole(t, f, "keyholder", model.PermTimesheetReadOwn, right)

		if _, err := sessions.Login(context.Background(), "new.person@example.com", "anything", ""); err != nil {
			t.Fatalf("login: %v", err)
		}

		if got := roleOf(t, f, "new.person@example.com"); got != model.RoleUser {
			t.Errorf("with %s on the default role the account starts as %q, want %q",
				right, got, model.RoleUser)
		}
	}
}

// A default role that has been deleted since it was chosen leaves an arriving
// account with the everyday role rather than refusing it: a custom role can be
// deleted once nobody holds it, and the directory's next newcomer is nobody yet.
func TestADefaultRoleThatIsGoneFallsBackToTheEverydayOne(t *testing.T) {
	f := newSyncFixture(t, 0.5)

	f.directory.users = []service.ExternalUser{
		{ID: "uuid-1", Email: "arrived@example.com", Role: "deleted-since"},
	}

	if _, err := f.sync.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := roleOf(t, f.fixture, "arrived@example.com"); got != model.RoleUser {
		t.Errorf("the account starts as %q, want %q", got, model.RoleUser)
	}
}

// The settings refuse to store a default role that administers the installation,
// so the screen cannot say one thing while the server does another.
func TestTheDirectorysDefaultRoleMayNotAdminister(t *testing.T) {
	f := newFixture(t)
	customRole(t, f, "keyholder", model.PermTimesheetReadOwn, model.PermSettingsManage)

	settings := service.NewSettingsService(newStubSettings(), f.roleRepo, f.userRepo, "Test")

	config := model.DefaultLDAPConfig()
	config.DefaultRole = "keyholder"

	err := settings.SaveLDAP(context.Background(), config)

	detail, ok := apperror.Detail(err)
	if !ok || detail.Code != "directoryRoleAdministers" {
		t.Fatalf("storing an administering default role answered %v, want directoryRoleAdministers", err)
	}
}

// A first sign-in leaves the daily target to the default, as every other way
// an account is made does.
//
// An account created through the form, and one the synchronisation creates,
// store zero - "follow the default" - and the synchronisation's own comment
// says what the alternative cost: a pinned figure that no longer moves with the
// default and shows as a number where every other row shows "default". The
// sign-in path still wrote the default's value in, so the same person arriving
// by signing in rather than by a synchronisation got the pinned one.
func TestAFirstSignInLeavesTheDailyTargetToTheDefault(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "new.person@example.com", Name: "New Person",
	})

	if _, err := sessions.Login(context.Background(), "new.person@example.com", "anything", ""); err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := f.userRepo.GetByEmail(context.Background(), "new.person@example.com")
	if err != nil {
		t.Fatalf("no account was created: %v", err)
	}

	if user.DailyTargetHours != 0 {
		t.Errorf("the account starts with a daily target of %v pinned, want 0, which "+
			"follows the default", user.DailyTargetHours)
	}
}
