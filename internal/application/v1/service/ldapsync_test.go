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

	// failAfter refuses once this many accounts have gone, standing in for a
	// database that becomes unreachable partway down the list.
	failAfter int
}

func (p *recordingPurger) PurgeUser(ctx context.Context, userID uint) error {
	if p.failAfter > 0 && len(p.purged) >= p.failAfter {
		return errors.New("the database went away")
	}

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
			f.timesheetRepo, purger, ratio, model.RoleUser),
	}
}

// externalUser creates a directory-backed account.
func externalUser(t *testing.T, f *fixture, email string) uint {
	t.Helper()

	created, err := f.users.CreateUser(context.Background(), command.CreateUserCommand{
		Name: email, Email: email, Role: model.RoleUser,
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

// A run that fails partway still says whom it had already deleted.
//
// This is the one operation in the application that removes people together with
// the hours they recorded, and the record of it is a log line per account written
// by the caller from report.Deleted. On the failure path the report was thrown
// away - `return nil, err` - so a run that purged three accounts and then lost
// the database logged "directory sync failed" and nothing else. The three are
// gone, irreversibly, and the only place that named them was a metric counter.
//
// Whoever runs this then has an error, a user list that is short by an unknown
// number, and no way to find out which without comparing against a backup.
func TestAFailedSyncStillReportsWhatItAlreadyDeleted(t *testing.T) {
	f := newSyncFixture(t, 1)

	first := externalUser(t, f.fixture, "aaa@example.com")
	externalUser(t, f.fixture, "bbb@example.com")

	// Nobody is in the directory any more, so both are candidates - but the
	// directory answers with somebody, or the run would refuse before deleting.
	f.directory.users = []service.ExternalUser{{ID: "kept", Email: "kept@example.com"}}

	// The first goes; the second is where the database is lost.
	f.purger.failAfter = 1

	report, err := f.sync.Sync(context.Background())
	if err == nil {
		t.Fatal("a purge that failed reported success")
	}

	if report == nil {
		t.Fatal("a run that deleted an account before failing reported nothing " +
			"about it; the account and its hours are gone and nothing names them")
	}

	if len(report.Deleted) != 1 {
		t.Fatalf("the report names %d deletion(s), want the 1 that happened",
			len(report.Deleted))
	}

	if report.Deleted[0].UserID != first {
		t.Errorf("the report names account %d, want %d",
			report.Deleted[0].UserID, first)
	}
}

// The same person twice in one directory answer creates them once.
//
// A search that matches an entry in two organisational units, or a filter that
// joins a group membership, returns duplicates - which is a shape of answer this
// has to survive rather than a shape it can rule out. It did not: the "already
// known" test is built once from the local accounts at the start of the run and
// was never told about the accounts the run itself had just created, so the
// second copy was treated as new, and saving it was refused as a duplicate
// address. That error aborted the whole run.
//
// Aborting is the expensive part. It happens after some accounts have been
// created and, on a run that also had deletions to make, before any of them - so
// a directory with one duplicate in it quietly stops reconciling, and the reason
// on the screen is "a user with that email already exists", which reads as a
// problem with the address rather than with the answer.
func TestTheSameAddressTwiceInTheDirectoryCreatesOneAccount(t *testing.T) {
	f := newSyncFixture(t, 0.5)

	f.directory.users = []service.ExternalUser{
		{ID: "one", Name: "Nils", Email: "nils@example.com"},
		{ID: "two", Name: "Nils", Email: "nils@example.com"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("a directory answer with a repeated address failed the run: %v", err)
	}

	if len(report.Created) != 1 {
		t.Errorf("the report names %d creation(s) for one person, want 1: %v",
			len(report.Created), report.Created)
	}

	all, err := f.userRepo.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	seen := 0

	for _, user := range all {
		if user.Email == "nils@example.com" {
			seen++
		}
	}

	if seen != 1 {
		t.Errorf("%d accounts exist for one directory entry, want 1", seen)
	}
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

	// Two recorded entries that a real run would destroy. Without a project: the
	// fixture's belongs to somebody else, and a project belongs to one person now, so
	// booking a departing colleague's hours onto it is refused - correctly. What this
	// case is about is that the hours exist and would be counted.
	for _, d := range []int{15, 16} {
		if _, err := f.timesheets.CreateTimesheet(context.Background(), command.CreateTimesheetCommand{
			UserID: leaving, Date: day(d), DurationHours: 4,
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
