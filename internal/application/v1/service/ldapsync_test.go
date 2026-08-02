package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// fakeDirectory stands in for LDAP.
type fakeDirectory struct {
	enabled bool
	users   []service.ExternalUser
	err     error
}

func (d *fakeDirectory) Enabled() bool { return d.enabled }

func (d *fakeDirectory) ListUsers(context.Context) ([]service.ExternalUser, error) {
	if d.err != nil {
		return nil, d.err
	}

	return d.users, nil
}

// recordingPurger notes what was deleted and removes it from the in-memory
// repository, standing in for the SQL cascade.
type recordingPurger struct {
	users  *memory.UserRepository
	purged []uint
}

func (p *recordingPurger) PurgeUser(ctx context.Context, userID uint) error {
	p.purged = append(p.purged, userID)

	return p.users.Delete(ctx, userID)
}

// syncFixture wires the sync service over the shared in-memory fixture.
type syncFixture struct {
	*fixture

	directory *fakeDirectory
	purger    *recordingPurger
	sync      *service.LDAPSyncService
}

func newSyncFixture(t *testing.T, ratio float64) *syncFixture {
	t.Helper()

	f := newFixture(t)
	directory := &fakeDirectory{enabled: true}
	purger := &recordingPurger{users: f.userRepo}

	return &syncFixture{
		fixture:   f,
		directory: directory,
		purger:    purger,
		sync: service.NewLDAPSyncService(directory, f.userRepo, f.roleRepo,
			f.timesheetRepo, purger, ratio, model.RoleEmployee),
	}
}

// externalUser creates a directory-backed account.
func externalUser(t *testing.T, f *fixture, email string) uint {
	t.Helper()

	created, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: email, Email: email, Role: model.RoleEmployee,
	})
	if err != nil {
		t.Fatalf("create %s: %v", email, err)
	}

	// The application marks LDAP accounts external; the service only ever
	// touches those.
	user, err := f.userRepo.GetByID(context.Background(), created.Result.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	user.IsExternal = true

	if _, err := f.userRepo.Update(context.Background(), user); err != nil {
		t.Fatalf("mark external: %v", err)
	}

	return created.Result.ID
}

// A directory that answers with nothing is almost certainly broken, and must
// never be read as "everybody left".
func TestSyncRefusesWhenDirectoryIsEmpty(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	externalUser(t, f.fixture, "gone@example.com")

	f.directory.users = nil

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.Aborted == "" {
		t.Fatal("an empty directory answer must abort the run")
	}

	if len(f.purger.purged) != 0 {
		t.Errorf("nothing may be deleted, but %d account(s) were", len(f.purger.purged))
	}
}

// A failed read is an error, never an empty result.
func TestSyncFailsWhenTheDirectoryCannotBeRead(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	externalUser(t, f.fixture, "gone@example.com")

	f.directory.err = errors.New("connection refused")

	if _, err := f.sync.Sync(context.Background()); err == nil {
		t.Fatal("a failed directory read must surface as an error")
	}

	if len(f.purger.purged) != 0 {
		t.Error("a failed read must not delete anyone")
	}
}

// A truncated or misfiltered answer would look like a mass departure, so a run
// above the configured share is refused.
func TestSyncRefusesAboveTheDeletionRatio(t *testing.T) {
	f := newSyncFixture(t, 0.5)

	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"} {
		externalUser(t, f.fixture, email)
	}

	// Only one of four still present: 75% would go, above the 50% limit.
	f.directory.users = []service.ExternalUser{{Email: "a@example.com"}}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.Aborted == "" {
		t.Fatal("a run above the ratio must abort")
	}

	if len(f.purger.purged) != 0 {
		t.Errorf("nothing may be deleted, but %d account(s) were", len(f.purger.purged))
	}
}

func TestSyncDeletesAccountsMissingUpstream(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	staying := externalUser(t, f.fixture, "staying@example.com")
	leaving := externalUser(t, f.fixture, "leaving@example.com")

	f.directory.users = []service.ExternalUser{{Email: "staying@example.com"}}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.Aborted != "" {
		t.Fatalf("unexpected abort: %s", report.Aborted)
	}

	if len(report.Deleted) != 1 || report.Deleted[0].UserID != leaving {
		t.Fatalf("expected exactly the departed account to be removed, got %+v", report.Deleted)
	}

	if _, err := f.userRepo.GetByID(context.Background(), leaving); err == nil {
		t.Error("the departed account should be gone")
	}

	if _, err := f.userRepo.GetByID(context.Background(), staying); err != nil {
		t.Errorf("the remaining account must be untouched: %v", err)
	}
}

// Local accounts were never in the directory, so its silence says nothing
// about them. The built-in administrator is likewise never removed.
func TestSyncLeavesLocalAndSystemAccountsAlone(t *testing.T) {
	f := newSyncFixture(t, 1.0)

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	// f.userID is the fixture's local account, never marked external.
	f.directory.users = []service.ExternalUser{{Email: "someone@example.com"}}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	for _, deleted := range report.Deleted {
		if deleted.UserID == f.userID {
			t.Error("a local account must never be deleted by a directory sync")
		}
	}

	if _, err := f.userRepo.GetByID(context.Background(), f.userID); err != nil {
		t.Errorf("the local account must survive: %v", err)
	}

	admin, err := f.userRepo.GetByEmail(context.Background(), service.SystemUserEmail)
	if err != nil {
		t.Fatalf("the built-in administrator must survive: %v", err)
	}

	if !admin.IsSystem {
		t.Error("expected the built-in administrator")
	}
}

// The preview must report exactly what a run would do, and change nothing.
func TestPreviewChangesNothingAndCountsTheDamage(t *testing.T) {
	f := newSyncFixture(t, 0.5)
	leaving := externalUser(t, f.fixture, "leaving@example.com")
	externalUser(t, f.fixture, "staying@example.com")

	// Two recorded entries that a real run would destroy.
	for _, d := range []int{15, 16} {
		if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
			UserID: leaving, ProjectID: f.projectID, Date: day(d), DurationHours: 4,
		}); err != nil {
			t.Fatalf("book: %v", err)
		}
	}

	f.directory.users = []service.ExternalUser{{Email: "staying@example.com"}}

	report, err := f.sync.Preview(context.Background())
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	if !report.DryRun {
		t.Error("a preview must report itself as one")
	}

	if len(report.Deleted) != 0 || len(f.purger.purged) != 0 {
		t.Error("a preview must not delete anything")
	}

	if len(report.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(report.Candidates))
	}

	if report.Candidates[0].Timesheets != 2 {
		t.Errorf("the preview must report the 2 entries at risk, reported %d",
			report.Candidates[0].Timesheets)
	}

	if _, err := f.userRepo.GetByID(context.Background(), leaving); err != nil {
		t.Errorf("the account must still exist after a preview: %v", err)
	}
}

// Accounts the directory has and this installation does not are added.
func TestSyncCreatesMissingAccounts(t *testing.T) {
	f := newSyncFixture(t, 0.5)

	f.directory.users = []service.ExternalUser{{Email: "new@example.com", Name: "New Person"}}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "new@example.com" {
		t.Fatalf("expected the new account to be created, got %+v", report.Created)
	}

	created, err := f.userRepo.GetByEmail(context.Background(), "new@example.com")
	if err != nil {
		t.Fatalf("the account should exist: %v", err)
	}

	if !created.IsExternal {
		t.Error("a directory-provisioned account must be marked external")
	}
}
