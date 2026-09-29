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
// Both lookups below had one answer for "there is none" and "it could not be
// read", and in both places the second is the dangerous reading: a database that
// hiccups for one query is not evidence of anything, and a guard that treats it
// as "nothing to guard" opens exactly when nobody is looking.

// unreadableRoles answers every role lookup with a database failure.
type unreadableRoles struct{ repository.RoleRepository }

func (unreadableRoles) GetByID(context.Context, uint) (*model.Role, error) {
	return nil, apperror.Internal(errors.New("the database did not answer"))
}

// unreadableIdentifiers answers a lookup by directory identifier with a
// database failure, and everything else as the repository does.
type unreadableIdentifiers struct{ repository.UserRepository }

func (unreadableIdentifiers) GetByExternalID(context.Context, string) (*model.User, error) {
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

// A sign-in that cannot look its person up by identifier does not go on as if
// they were new.
//
// After a renamed mailbox the identifier is the only thing that finds the
// account; the address is new. Read as "no such person", a failed lookup went on
// to the address, found nothing under the new one, and created a second account
// for the same directory entry - an empty one, which the person was then signed
// in to, their hours still under the first.
func TestADirectorySignInDoesNotCreateASecondAccountWhenTheLookupFails(t *testing.T) {
	f := newFixture(t)
	externalUserWithID(t, f, "maiden.name@example.com", "uuid-1")

	sessions := service.NewSessionService(unreadableIdentifiers{f.userRepo}, f.roleRepo,
		newStubSessions(), f.auth, time.Hour).
		WithExternalAuth(&fakeAuthenticator{user: &service.ExternalUser{
			ID: "uuid-1", Email: "married.name@example.com", Name: "Sam Taylor",
		}}, model.RoleUser)

	if _, err := sessions.Login(context.Background(), "married.name@example.com", "anything", ""); err == nil {
		t.Error("the sign-in went on without being able to read who this is")
	}

	everyone, err := f.userRepo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("listing accounts: %v", err)
	}

	for _, user := range everyone {
		if user.Email == "married.name@example.com" {
			t.Errorf("a second account was created for the same directory entry: %+v", user)
		}
	}
}
