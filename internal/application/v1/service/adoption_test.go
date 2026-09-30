package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// passwordDirectory answers a sign-in with its entry only for its own password,
// as a real directory does.
type passwordDirectory struct {
	user     *service.ExternalUser
	password string
}

func (d *passwordDirectory) Enabled() bool { return true }

func (d *passwordDirectory) Authenticate(
	_ context.Context, _, password string,
) (*service.ExternalUser, bool, error) {
	if password != d.password {
		return nil, false, nil
	}

	return d.user, true, nil
}

// An account the directory signs its owner in to is the directory's from then on.
//
// Matching by address is how an installation moves from local passwords to a
// directory, and the move stopped half-way: the account kept everything local it
// had. One an administrator created without a password kept the documented
// initial password and the demand to replace it, so its owner - signing in with
// the directory's password - was sent to change a password they did not have,
// on every sign-in. Worse, that password still worked: the directory is asked
// first, and when it refuses, a local account is checked against its own hash,
// so anybody who had read the README could sign in as them with changeme123. And
// a synchronisation only removes directory accounts, so it would have survived
// its owner leaving.
func TestAnAdoptedAccountIsTheDirectorysFromThenOn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	created, err := f.users.CreateUser(ctx, command.CreateUserCommand{
		Name: "Worker", Email: "worker@example.com", Role: model.RoleUser,
	})
	if err != nil {
		t.Fatalf("create the account: %v", err)
	}

	sessions := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour).
		WithExternalAuth(&passwordDirectory{
			user:     &service.ExternalUser{ID: "uuid-worker", Email: "worker@example.com"},
			password: "the-directory-password",
		}, model.RoleUser)

	if _, err := sessions.Login(ctx, "worker@example.com", "the-directory-password", ""); err != nil {
		t.Fatalf("the directory's own password was refused: %v", err)
	}

	adopted, err := f.userRepo.GetByID(ctx, created.Result.ID)
	if err != nil {
		t.Fatalf("read the account back: %v", err)
	}

	if !adopted.IsExternal {
		t.Error("the account the directory signed its owner in to is still a local one")
	}

	if adopted.MustChangePassword {
		t.Error("the directory's account still demands a new local password on every sign-in")
	}

	if _, err := sessions.Login(ctx, "worker@example.com", service.SystemUserPassword, ""); err == nil {
		t.Error("the documented initial password still opens an account the directory owns")
	}
}
