package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// A directory sign-in reads a failure to look something up as a failure, never
// as the thing not being there.
//
// The lookup below had one answer for "there is none" and "it could not be
// read", and the second is the dangerous reading: a database that hiccups for
// one query is not evidence of anything, and a guard that treats it as "nothing
// to guard" opens exactly when nobody is looking.

// unreadableRoles answers every role lookup with a database failure.
type unreadableRoles struct{ repository.RoleRepository }

func (unreadableRoles) GetByID(context.Context, uint) (*model.Role, error) {
	return nil, apperror.Internal(errors.New("the database did not answer"))
}

// The guard that keeps a directory entry from claiming a local administrator
// asked the administrator's role, and read a role it could not read as "no
// rights" - which is right for a role that has been deleted and is what the
// comment on it argued, and wrong for one the database failed to return: the
// administrator was then adopted by whoever wrote that address into the
// directory, and signed in with everything they hold.
func TestADirectoryEntryCannotClaimAnAdministratorWhoseRoleCannotBeRead(t *testing.T) {
	f := newFixture(t)
	admin := localUserWithRole(t, f, "chief@example.com", model.RoleAdmin)

	sessions := service.NewSessionService(f.userRepo, unreadableRoles{f.roleRepo}, newStubSessions(),
		f.auth, time.Hour).
		WithExternalAuth(&fakeAuthenticator{user: &service.ExternalUser{
			ID: "uuid-intruder", Email: "chief@example.com", Name: "Not Them",
		}}, model.RoleUser)

	if _, err := sessions.Login(context.Background(), "chief@example.com", "anything", ""); err == nil {
		t.Error("the directory signed in as a local administrator while the role could not be read")
	}

	unchanged, err := f.userRepo.GetByID(context.Background(), admin)
	if err != nil {
		t.Fatalf("the account must still exist: %v", err)
	}

	if unchanged.ExternalID != "" {
		t.Errorf("the directory stamped its identifier on an administrator: %q", unchanged.ExternalID)
	}
}
