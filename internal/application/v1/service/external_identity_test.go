package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// These tests all guard one property: an account is recognised by the
// identifier the directory assigned it, not by its mail address. Matching on
// the address means a rename reads as "this person left and a stranger
// arrived" - and the departure half of that deletes their recorded hours.

// externalUserWithID creates a directory-backed account that already carries
// the directory's identifier.
func externalUserWithID(t *testing.T, f *fixture, email, externalID string) uint {
	t.Helper()

	id := externalUser(t, f, email)

	user, err := f.userRepo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read back %s: %v", email, err)
	}

	user.ExternalID = externalID

	if _, err := f.userRepo.Update(context.Background(), user); err != nil {
		t.Fatalf("store identifier for %s: %v", email, err)
	}

	return id
}

// The whole point of the identifier: the address changed upstream, the person
// did not.
func TestSyncFollowsARenamedMailbox(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	id := externalUserWithID(t, f.fixture, "maiden.name@example.com", "uuid-1")

	// Same entry, new address.
	f.directory.users = []service.ExternalUser{
		{ID: "uuid-1", Email: "married.name@example.com", Name: "Sam Taylor"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Candidates) != 0 {
		t.Fatalf("a rename is not a departure, but %+v was marked for deletion", report.Candidates)
	}

	if len(f.purger.purged) != 0 {
		t.Error("a rename must not delete the account or its recorded hours")
	}

	if _, err := f.userRepo.GetByID(context.Background(), id); err != nil {
		t.Errorf("the account must survive a rename: %v", err)
	}
}

// The rename must not create a second account under the new address either -
// that would split one person's hours across two logins.
func TestSyncDoesNotDuplicateARenamedAccount(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	externalUserWithID(t, f.fixture, "maiden.name@example.com", "uuid-1")

	f.directory.users = []service.ExternalUser{
		{ID: "uuid-1", Email: "married.name@example.com"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("the renamed entry is already known, but %+v was created", report.Created)
	}
}

// Losing the identifier is the dangerous direction: an account that has one
// must not silently fall back to the address, or a reused mailbox would keep a
// departed person's account alive.
func TestSyncDeletesWhenTheIdentifierIsGoneEvenIfTheAddressRemains(t *testing.T) {
	f := newSyncFixture(t, 1.0)
	left := externalUserWithID(t, f.fixture, "shared@example.com", "uuid-departed")

	// The address is still in the directory, but it now belongs to a different
	// entry - a successor who inherited the mailbox.
	f.directory.users = []service.ExternalUser{
		{ID: "uuid-successor", Email: "shared@example.com"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Deleted) != 1 || report.Deleted[0].UserID != left {
		t.Fatalf("the departed account should be removed, got %+v", report.Deleted)
	}
}

// Installations that predate the identifier have accounts without one. They
// must keep working on the mail address until a sign-in adopts an identifier.
func TestSyncStillMatchesAccountsWithoutAnIdentifier(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	legacy := externalUser(t, f.fixture, "legacy@example.com")

	f.directory.users = []service.ExternalUser{
		{ID: "uuid-1", Email: "legacy@example.com"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Candidates) != 0 {
		t.Fatalf("an account without an identifier must match on its address, got %+v", report.Candidates)
	}

	if len(report.Created) != 0 {
		t.Errorf("the account already exists, but %+v was created", report.Created)
	}

	if _, err := f.userRepo.GetByID(context.Background(), legacy); err != nil {
		t.Errorf("the legacy account must survive: %v", err)
	}
}

// ---------------------------------------------------------------- sign-in

// stubSessions is enough of a session store for a sign-in to complete; the
// tests below care about the account it resolves to, not about the session.
type stubSessions struct {
	mu    sync.Mutex
	items map[string]*model.Session
}

func newStubSessions() *stubSessions {
	return &stubSessions{items: make(map[string]*model.Session)}
}

func (s *stubSessions) Save(_ context.Context, session *model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[session.TokenHash] = session

	return nil
}

func (s *stubSessions) Get(_ context.Context, tokenHash string) (*model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.items[tokenHash]
	if !ok {
		return nil, apperror.NotFound("session", tokenHash)
	}

	return session, nil
}

func (s *stubSessions) Delete(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items, tokenHash)

	return nil
}

func (s *stubSessions) DeleteForUser(_ context.Context, userID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for hash, session := range s.items {
		if session.UserID == userID {
			delete(s.items, hash)
		}
	}

	return nil
}

func (s *stubSessions) DeleteExpired(context.Context) (int64, error) { return 0, nil }

// fakeAuthenticator answers a sign-in with a fixed directory entry.
type fakeAuthenticator struct{ user *service.ExternalUser }

func (a *fakeAuthenticator) Enabled() bool { return true }

func (a *fakeAuthenticator) Authenticate(
	context.Context, string, string,
) (*service.ExternalUser, bool, error) {
	return a.user, true, nil
}

// newSessionFixture wires a session service that authenticates against the
// given directory entry.
func newSessionFixture(t *testing.T, directoryUser *service.ExternalUser) (*fixture, *service.SessionService) {
	t.Helper()

	f := newFixture(t)
	sessions := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(),
		f.auth, time.Hour).
		WithExternalAuth(&fakeAuthenticator{user: directoryUser}, model.RoleEmployee)

	return f, sessions
}

// A first sign-in records the identifier, so the next rename can be followed.
func TestFirstSignInRecordsTheIdentifier(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "new.hire@example.com", Name: "New Hire",
	})

	if _, err := sessions.Login(context.Background(), "new.hire@example.com", "anything", ""); err != nil {
		t.Fatalf("login: %v", err)
	}

	created, err := f.userRepo.GetByEmail(context.Background(), "new.hire@example.com")
	if err != nil {
		t.Fatalf("the account should have been provisioned: %v", err)
	}

	if created.ExternalID != "uuid-1" {
		t.Errorf("expected the identifier to be stored, got %q", created.ExternalID)
	}
}

// An account created before identifiers were recorded adopts one on the next
// sign-in, without becoming a second account.
func TestSignInAdoptsTheIdentifierForAnOlderAccount(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "legacy@example.com",
	})

	existing := externalUser(t, f, "legacy@example.com")

	if _, err := sessions.Login(context.Background(), "legacy@example.com", "anything", ""); err != nil {
		t.Fatalf("login: %v", err)
	}

	adopted, err := f.userRepo.GetByID(context.Background(), existing)
	if err != nil {
		t.Fatalf("the account must still exist: %v", err)
	}

	if adopted.ExternalID != "uuid-1" {
		t.Errorf("expected the identifier to be adopted, got %q", adopted.ExternalID)
	}

	all, err := f.userRepo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	if count := countWithEmail(all, "legacy@example.com"); count != 1 {
		t.Errorf("expected one account for the address, found %d", count)
	}
}

// Signing in after a rename lands on the same account, and the stored address
// follows the directory rather than staying stale.
func TestSignInAfterARenameKeepsTheSameAccount(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "married.name@example.com", Name: "Sam Taylor",
	})

	existing := externalUserWithID(t, f, "maiden.name@example.com", "uuid-1")

	result, err := sessions.Login(context.Background(), "married.name@example.com", "anything", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Principal.User.ID != existing {
		t.Fatalf("expected to sign in as the existing account %d, got %d",
			existing, result.Principal.User.ID)
	}

	updated, err := f.userRepo.GetByID(context.Background(), existing)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if updated.Email != "married.name@example.com" {
		t.Errorf("expected the stored address to follow the directory, got %q", updated.Email)
	}

	if updated.Name != "Sam Taylor" {
		t.Errorf("expected the stored name to follow the directory, got %q", updated.Name)
	}
}

func countWithEmail(users []*model.User, email string) int {
	var count int

	for _, user := range users {
		if user.Email == email {
			count++
		}
	}

	return count
}
