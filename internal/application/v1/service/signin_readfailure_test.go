package service_test

import (
	"context"
	"errors"
	"strings"
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

// unreadableAddresses answers a lookup by address with a database failure.
type unreadableAddresses struct{ repository.UserRepository }

func (unreadableAddresses) GetByEmail(context.Context, string) (*model.User, error) {
	return nil, apperror.Internal(errors.New("the database did not answer"))
}

// counterSpy keeps the labels of every counter incremented, by name.
type counterSpy struct {
	counted map[string][]string
}

func (c *counterSpy) IncrementCounter(_ context.Context, name string, labels ...string) {
	c.counted[name] = append(c.counted[name], strings.Join(labels, "="))
}

func (*counterSpy) RecordHistogram(context.Context, string, float64, ...string) {}

// A password sign-in that cannot read the account it is for is a database that
// failed, not a wrong password - in what it answers and in what it counts.
//
// It was read as no such account: the sign-in was refused as wrong credentials,
// which the handler does not log because a typo is the ordinary case, and the
// metric counted it where a password list being worked through is counted. A
// database that stopped answering showed as nobody being able to type their
// password, and nowhere as itself.
func TestASignInTheDatabaseCannotAnswerIsCountedAsTheDatabase(t *testing.T) {
	f := newFixture(t)
	spy := &counterSpy{counted: map[string][]string{}}

	sessions := service.NewSessionService(unreadableAddresses{f.userRepo}, f.roleRepo,
		newStubSessions(), f.auth, time.Hour).WithMetrics(spy)

	_, err := sessions.Login(context.Background(), "someone@example.com", "a-password", "")
	if apperror.KindOf(err) != apperror.KindInternal {
		t.Errorf("the sign-in answered %v, want an internal failure the handler logs", err)
	}

	if got := spy.counted[service.MetricSignInFailures]; len(got) != 1 || got[0] != "reason=database" {
		t.Errorf("the refusal was counted as %v, want reason=database", got)
	}
}

// The same for a ticket, whose account is looked up after the directory has
// answered: a lookup that failed there was counted as nothing at all.
func TestATicketSignInTheDatabaseCannotAnswerIsCountedAsTheDatabase(t *testing.T) {
	f := newFixture(t)
	spy := &counterSpy{counted: map[string][]string{}}

	sessions := service.NewSessionService(unreadableIdentifiers{f.userRepo}, f.roleRepo,
		newStubSessions(), f.auth, time.Hour).
		WithExternalAuth(&lookupDirectory{entries: map[string]*service.ExternalUser{
			"jdoe": {ID: "uuid-jdoe", Email: "jdoe@example.com"},
		}}, model.RoleUser).
		WithMetrics(spy)

	if _, err := sessions.KerberosLogin(context.Background(), "jdoe", "EXAMPLE.COM", ""); err == nil {
		t.Fatal("a ticket sign-in whose account could not be read was let through")
	}

	if got := spy.counted[service.MetricSignInFailures]; len(got) != 1 || got[0] != "reason=database" {
		t.Errorf("the refusal was counted as %v, want reason=database", got)
	}
}

// unansweringDirectory is a directory that is configured and does not answer.
type unansweringDirectory struct{}

func (unansweringDirectory) Enabled() bool { return true }

func (unansweringDirectory) Authenticate(context.Context, string, string) (*service.ExternalUser, bool, error) {
	return nil, false, errors.New("dial tcp: connection refused")
}

// And a directory that does not answer is counted as the directory, apart from
// the database, because the two are fixed by different people.
func TestASignInTheDirectoryCannotAnswerIsCountedAsTheDirectory(t *testing.T) {
	f := newFixture(t)
	spy := &counterSpy{counted: map[string][]string{}}

	sessions := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour).
		WithExternalAuth(unansweringDirectory{}, model.RoleUser).
		WithMetrics(spy)

	_, err := sessions.Login(context.Background(), "someone@example.com", "a-password", "")
	if apperror.KindOf(err) != apperror.KindInternal {
		t.Errorf("the sign-in answered %v, want an internal failure the handler logs", err)
	}

	if got := spy.counted[service.MetricSignInFailures]; len(got) != 1 || got[0] != "reason=directory" {
		t.Errorf("the refusal was counted as %v, want reason=directory", got)
	}
}
