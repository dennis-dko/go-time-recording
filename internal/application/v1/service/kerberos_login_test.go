package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// lookupDirectory holds entries by the name they are looked up with and turns
// every password away: a Kerberos sign-in never offers one.
type lookupDirectory struct {
	entries map[string]*service.ExternalUser
}

func (d *lookupDirectory) Enabled() bool { return true }

func (d *lookupDirectory) Authenticate(context.Context, string, string) (*service.ExternalUser, bool, error) {
	return nil, false, nil
}

func (d *lookupDirectory) Lookup(_ context.Context, login string) (*service.ExternalUser, bool, error) {
	entry, found := d.entries[login]

	return entry, found, nil
}

func kerberosSessions(t *testing.T, entries map[string]*service.ExternalUser) (*fixture, *service.SessionService) {
	t.Helper()

	f := newFixture(t)

	return f, service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour).
		WithExternalAuth(&lookupDirectory{entries: entries}, model.RoleUser)
}

// A Kerberos sign-in reaches the account a directory password sign-in reaches,
// making it the first time as that does.
func TestAKerberosSignInReachesTheDirectorysAccount(t *testing.T) {
	ctx := context.Background()

	_, sessions := kerberosSessions(t, map[string]*service.ExternalUser{
		"jdoe": {ID: "uuid-jdoe", Email: "jdoe@example.com", Name: "Jane Doe"},
	})

	first, err := sessions.KerberosLogin(ctx, "jdoe", "EXAMPLE.COM", "")
	if err != nil {
		t.Fatalf("a ticket for somebody the directory holds was refused: %v", err)
	}

	account := first.Principal.User
	if !account.IsExternal || account.Email != "jdoe@example.com" || account.ExternalID != "uuid-jdoe" {
		t.Errorf("the ticket reached %+v rather than the directory's account for jdoe", *account)
	}

	second, err := sessions.KerberosLogin(ctx, "jdoe", "EXAMPLE.COM", "")
	if err != nil {
		t.Fatalf("the second sign-in was refused: %v", err)
	}

	if second.Principal.User.ID != account.ID {
		t.Errorf("the second sign-in reached account %d, the first %d", second.Principal.User.ID, account.ID)
	}
}

// Active Directory writes a userPrincipalName as name@realm, and a sign-in filter
// may ask for that rather than the bare name.
func TestAKerberosSignInFindsTheNameWithItsRealm(t *testing.T) {
	_, sessions := kerberosSessions(t, map[string]*service.ExternalUser{
		"jdoe@EXAMPLE.COM": {ID: "uuid-jdoe", Email: "jdoe@example.com"},
	})

	if _, err := sessions.KerberosLogin(context.Background(), "jdoe", "EXAMPLE.COM", ""); err != nil {
		t.Errorf("an entry held under name@realm was not found: %v", err)
	}
}

func TestAPrincipalTheDirectoryDoesNotHoldIsRefused(t *testing.T) {
	_, sessions := kerberosSessions(t, map[string]*service.ExternalUser{})

	_, err := sessions.KerberosLogin(context.Background(), "stranger", "EXAMPLE.COM", "")
	if !hasCode(err, "invalidCredentials") {
		t.Errorf("a principal the directory does not hold was answered %v", err)
	}
}

// Without a directory there is nobody to look the name up with, and a name on its
// own is not an account.
func TestAKerberosSignInWithoutADirectoryIsRefused(t *testing.T) {
	f := newFixture(t)
	sessions := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour)

	_, err := sessions.KerberosLogin(context.Background(), "jdoe", "EXAMPLE.COM", "")
	if !hasCode(err, "kerberosNeedsDirectory") {
		t.Errorf("a Kerberos sign-in without a directory was answered %v", err)
	}
}

// The built-in administrator is local and only local, whatever a ticket says.
func TestAKerberosSignInNeverReachesTheBuiltInAdministrator(t *testing.T) {
	_, sessions := kerberosSessions(t, map[string]*service.ExternalUser{
		"admin": {ID: "uuid-admin", Email: service.SystemUserEmail},
	})

	if _, err := sessions.KerberosLogin(context.Background(), "admin", "EXAMPLE.COM", ""); err == nil {
		t.Error("a ticket signed in as the built-in administrator")
	}
}

// A second factor its owner enrolled is asked for: the ticket stands in for the
// password and for nothing else.
func TestAKerberosSignInAsksForAnEnrolledSecondFactor(t *testing.T) {
	ctx := context.Background()

	f, sessions := kerberosSessions(t, map[string]*service.ExternalUser{
		"jdoe": {ID: "uuid-jdoe", Email: "jdoe@example.com"},
	})

	first, err := sessions.KerberosLogin(ctx, "jdoe", "EXAMPLE.COM", "")
	if err != nil {
		t.Fatal(err)
	}

	secret, err := security.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}

	if err := f.userRepo.SetTOTP(ctx, first.Principal.User.ID, secret, true); err != nil {
		t.Fatal(err)
	}

	if _, err := sessions.KerberosLogin(ctx, "jdoe", "EXAMPLE.COM", ""); !errors.Is(err, service.ErrTOTPRequired) {
		t.Fatalf("an enrolled second factor was not asked for: %v", err)
	}

	code, err := security.CurrentTOTPCode(secret)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sessions.KerberosLogin(ctx, "jdoe", "EXAMPLE.COM", code); err != nil {
		t.Errorf("the ticket and the right code were refused: %v", err)
	}
}

// An account the directory has switched off is not signed in by a ticket.
//
// A password sign-in learns it from the bind, which a disabled Active Directory
// account fails at once. A ticket was issued before the account was switched off
// and stays valid for hours - the browser presents the service ticket it already
// holds, and it is checked here with the keytab, offline - so the entry is all
// there is to go on, and the ticket path read only that the entry was there.
func TestAKerberosSignInIsRefusedForAnAccountTheDirectoryHasSwitchedOff(t *testing.T) {
	_, sessions := kerberosSessions(t, map[string]*service.ExternalUser{
		"jdoe": {ID: "uuid-jdoe", Email: "jdoe@example.com", Name: "Jane Doe", Disabled: true},
	})

	if _, err := sessions.KerberosLogin(context.Background(), "jdoe", "EXAMPLE.COM", ""); err == nil {
		t.Fatal("a ticket opened a session for an account the directory has switched off")
	}
}
