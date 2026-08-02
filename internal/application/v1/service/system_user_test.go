package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The built-in administrator exists so an installation always has a way back
// in. That only holds if it stays local and stays present: an account the
// directory can take over, or that an administrator can delete, is not a way
// back in at all.

func ensureSystemUser(t *testing.T, f *fixture) *model.User {
	t.Helper()

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	user, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
	if err != nil {
		t.Fatalf("the built-in administrator should exist: %v", err)
	}

	return user
}

// Whoever controls the directory must not be able to sign in as the built-in
// administrator by creating an entry with its address.
func TestDirectoryCannotImpersonateTheSystemUser(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-attacker", Email: service.SystemUserEmail, Name: "Not The Administrator",
	})

	ensureSystemUser(t, f)

	// The fake directory accepts any password, so a successful sign-in here
	// would mean the directory decided who the administrator is.
	_, err := sessions.Login(context.Background(), service.SystemUserEmail, "whatever", "")
	if err == nil {
		t.Fatal("the directory must never authenticate the built-in administrator")
	}

	user, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
	if err != nil {
		t.Fatalf("the account must still exist: %v", err)
	}

	if user.IsExternal || user.ExternalID != "" {
		t.Errorf("the built-in administrator must stay local, got isExternal=%v externalId=%q",
			user.IsExternal, user.ExternalID)
	}
}

// With a directory configured, the built-in administrator still signs in with
// its local password - that is the whole point of it.
func TestSystemUserStillSignsInLocallyWithADirectoryConfigured(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-someone", Email: "someone@example.com",
	})

	ensureSystemUser(t, f)

	result, err := sessions.Login(context.Background(),
		service.SystemUserEmail, service.SystemUserPassword, "")
	if err != nil {
		t.Fatalf("the built-in administrator must be able to sign in locally: %v", err)
	}

	if !result.Principal.User.IsSystem {
		t.Error("expected to be signed in as the built-in administrator")
	}
}

// A row that was tampered with, or a sync that overreached, is corrected on the
// next start rather than leaving the account owned by the directory.
func TestStartupRepairsASystemUserMarkedExternal(t *testing.T) {
	f := newFixture(t)
	user := ensureSystemUser(t, f)

	user.IsExternal = true
	user.ExternalID = "uuid-hijack"

	if _, err := f.userRepo.Update(context.Background(), user); err != nil {
		t.Fatalf("simulate tampering: %v", err)
	}

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	repaired, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if repaired.IsExternal || repaired.ExternalID != "" {
		t.Errorf("expected the account to be restored to local, got isExternal=%v externalId=%q",
			repaired.IsExternal, repaired.ExternalID)
	}

	if !repaired.IsSystem {
		t.Error("expected the account to remain the built-in administrator")
	}
}

// Deleting it would leave an installation with no guaranteed way in.
func TestSystemUserCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	user := ensureSystemUser(t, f)

	err := f.users.DeleteUser(context.Background(), command.DeleteUserCommand{ID: user.ID})
	if err == nil {
		t.Fatal("deleting the built-in administrator must be refused")
	}

	if _, err := f.userRepo.GetByID(context.Background(), user.ID); err != nil {
		t.Errorf("the account must survive the attempt: %v", err)
	}
}

// EnsureSystemUser runs on every start; a second call must not create a second
// administrator or reset the password of the existing one.
func TestEnsureSystemUserIsIdempotent(t *testing.T) {
	f := newFixture(t)

	created, err := f.auth.EnsureSystemUser(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	if !created {
		t.Fatal("the first call should report that it created the account")
	}

	if err := f.auth.ChangePassword(context.Background(),
		mustSystemUser(t, f).ID, service.SystemUserPassword, "a-better-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	createdAgain, err := f.auth.EnsureSystemUser(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if createdAgain {
		t.Error("the second call must not create another administrator")
	}

	all, err := f.userRepo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	if count := countWithEmail(all, service.SystemUserEmail); count != 1 {
		t.Errorf("expected exactly one built-in administrator, found %d", count)
	}

	// The changed password must have survived, or every restart would hand the
	// installation back to the documented default.
	sessions := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour)
	if _, err := sessions.Login(context.Background(),
		service.SystemUserEmail, "a-better-password", ""); err != nil {
		t.Errorf("the changed password must still work after a restart: %v", err)
	}
}

func mustSystemUser(t *testing.T, f *fixture) *model.User {
	t.Helper()

	user, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
	if err != nil {
		t.Fatalf("the built-in administrator should exist: %v", err)
	}

	return user
}
